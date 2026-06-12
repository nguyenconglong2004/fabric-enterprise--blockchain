# Phân tích tối ưu tốc độ đóng block & đảm bảo đồng bộ dữ liệu

> Tài liệu này phân tích cơ chế đồng thuận, lưu trữ và đồng bộ dữ liệu (log/block) giữa các node
> trong Ordering Service, sau đó chỉ ra các điểm cần cải thiện để **tăng tốc độ đóng block**
> mà vẫn **đảm bảo đồng bộ dữ liệu & trạng thái** giữa các node.
>
> Mỗi điểm tối ưu đều ghi rõ **file / hàm / dòng** liên quan.

---

## Phần 1 — Cơ chế hiện tại (baseline)

### 1.1 Đường đi của một block (consensus pipeline)

```
Client/Core → SubmitTransaction → TxPool (leader)
                                      │
            StartAutoProposeBlock loop (mỗi AutoProposeInterval)
                                      │
                            ProposeBlock(batchSize)
                                      │
                       proposeBlockWithTxs:
                         - tạo Block (hash-chain, merkle)
                         - AppendEntry vào RaftLog (uncommitted)
                         - Broadcast MsgBlockProposal
                         - go waitForBlockAcks(entry)
                                      │
        Follower: HandleBlockProposal → kiểm tra PrevLogIndex/term
                         → AppendEntry → gửi MsgBlockProposalAck
                                      │
        Leader: waitForBlockAcks gom ACK → đạt majority → commitBlock
                                      │
                         commitBlock:
                         - OrderingBlock.AppendBlock
                         - setLastCommittedHash
                         - DeliverMgr.NotifyNewBlock
                         - xóa tx khỏi TxPool
                         - Broadcast MsgBlockCommit
                         - signal blockCommittedNotify
                                      │
        Follower: HandleBlockCommit → tìm entry trong RaftLog
                         → OrderingBlock.AppendBlock → setLastCommittedHash
```

**File chính:**
- [source/internal/raft/transaction.go](../source/internal/raft/transaction.go) — toàn bộ pipeline propose/commit
- [source/internal/raft/consensus.go](../source/internal/raft/consensus.go) — dispatcher message
- [source/internal/types/block.go](../source/internal/types/block.go) — `Block`, `RaftLog`, `OrderingBlock`

### 1.2 Lưu trữ

- `RaftLog` (uncommitted) và `OrderingBlock` (committed) đều **in-memory**, append-only.
  Xem [block.go:193-277](../source/internal/types/block.go#L193-L277).
- `lastCommittedHash` ([node.go:64](../source/internal/raft/node.go#L64)) là con trỏ hash-chain,
  chỉ tiến khi block được commit.

### 1.3 Đồng bộ (sync) khi join/rejoin

Pull-based, song song theo shard. Xem [sync.go](../source/internal/raft/sync.go), [sync_server.go](../source/internal/raft/sync_server.go).
1. Discovery: broadcast `MsgSyncStatusRequest`, gom `MsgSyncStatusResponse` trong cửa sổ `SyncDiscoveryWindow`.
2. Chọn target theo majority `(commitIndex, commitHash)` — `pickSyncTarget` [sync.go:202](../source/internal/raft/sync.go#L202).
3. Fetch song song theo shard `SyncShardSize=64` — `fetchBlocksParallel` [sync.go:263](../source/internal/raft/sync.go#L263).
4. Verify hash-chain — `verifyHashChain` [sync.go:443](../source/internal/raft/sync.go#L443).
5. Install block + log entries.

### 1.4 Giá trị config thực tế (lưu ý: lệch với bảng trong CLAUDE.md)

| Tham số | Giá trị code thực tế | Định nghĩa tại |
|---|---|---|
| `AutoProposeBlockSize` | **1000** tx/block | [node.go:23](../source/internal/raft/node.go#L23) |
| `AutoProposeInterval` | **100ms** | [node.go:24](../source/internal/raft/node.go#L24) |
| `HeartbeatInterval` | 2s | [protocol.go:10](../source/internal/network/protocol.go#L10) |
| `HeartbeatTimeout` | 5s | [protocol.go:11](../source/internal/network/protocol.go#L11) |
| `SyncShardSize` | 64 | [protocol.go:17](../source/internal/network/protocol.go#L17) |

> ⚠️ CLAUDE.md vẫn ghi `AutoProposeBlockSize=20`, `AutoProposeInterval=500ms`. Code đã đổi thành 1000 / 100ms. Cần cập nhật lại CLAUDE.md.

---

## Phần 2 — Các điểm tối ưu tốc độ đóng block

Xếp theo mức độ tác động (cao → thấp).

### 🔴 OPT-1 — Commit ngay khi đủ majority, bỏ độ trễ ticker 100ms

> ✅ **Đã triển khai.** Ticker 100ms đã bị loại; `waitForBlockAcks` kiểm tra `len(acks) >= majority`
> ngay trong nhánh nhận ACK và commit lập tức. Thêm fast-path self-majority (cluster 1 node) trước vòng lặp.
> Xem [transaction.go:238-288](../source/internal/raft/transaction.go#L238-L288).

**File/hàm:** `waitForBlockAcks` — [transaction.go:224-282](../source/internal/raft/transaction.go#L224-L282)

**Vấn đề:** Khi ACK đến qua `case msg := <-rn.BlockAckChan`, code chỉ **thêm vào map `acks`** chứ
**không kiểm tra majority ngay**. Việc kiểm tra majority chỉ xảy ra ở nhánh `case <-ticker.C` (ticker 100ms)
hoặc `case <-timeout`. Hệ quả: kể cả khi majority ACK về sau vài ms, block vẫn phải chờ tới tick kế tiếp
→ thêm **tới 100ms độ trễ commit cho mỗi block**.

```go
case msg := <-rn.BlockAckChan:
    ...
    acks[senderID] = true            // chỉ cộng dồn, KHÔNG check majority ở đây
    // → phải đợi <-ticker.C mới commit
```

**Cải thiện:** Sau khi tăng `acks`, kiểm tra `len(acks) >= majority` ngay trong nhánh nhận ACK và
commit lập tức rồi `return`. Khi đó ticker chỉ còn là cơ chế fallback. Loại bỏ tới 100ms/block.

---

### 🔴 OPT-2 — Pipeline block proposal (bỏ tuần tự "propose → chờ commit → propose")

> ✅ **Đã triển khai mức tối thiểu an toàn (event-driven, KHÔNG pipeline).** Loop auto-propose bỏ
> `time.After(interval)` vô điều kiện đầu vòng. Khi `len(TxPool) >= batchSize` → propose **ngay**, không
> chờ interval; interval chỉ còn là flush-timeout cho batch lẻ. Vẫn **1 block in-flight** (chờ
> `blockCommittedNotify`) → hash-chain/sync không đổi, không cần SYNC-1/2/5.
>
> Kết quả đo: trước fix ~7.2 blocks/s (139ms/block, bị 100ms interval chặn) → bỏ tax interval, nhịp đóng
> block bám theo commit-RTT (~vài chục ms) khi pool đầy. Xem [transaction.go](../source/internal/raft/transaction.go) `StartAutoProposeBlock`.
>
> **Chưa làm (pipeline thật):** nhiều block in-flight (PrevHash lấy từ entry cuối RaftLog thay vì
> committed hash) — đòi SYNC-1/SYNC-2/SYNC-5 trước. Chỉ cần khi trần commit-RTT × 1-in-flight không đủ.

**File/hàm:** `StartAutoProposeBlock` — [transaction.go:563-637](../source/internal/raft/transaction.go#L563-L637),
cụ thể đoạn [transaction.go:617-632](../source/internal/raft/transaction.go#L617-L632)

**Vấn đề:** Vòng lặp auto-propose **block (chặn)** sau mỗi lần propose để chờ `blockCommittedNotify`
(hoặc timeout 10s) rồi mới propose block tiếp theo:

```go
if err := rn.ProposeBlock(currentBatchSize); err != nil { ... }
select {
case <-rn.blockCommittedNotify:   // CHỜ commit xong mới qua vòng sau
case <-time.After(10 * time.Second):
}
```

Kết hợp với `time.After(AutoProposeInterval)` đầu vòng ([transaction.go:594](../source/internal/raft/transaction.go#L594)),
mỗi chu kỳ block ≈ `AutoProposeInterval (100ms) + RTT đồng thuận`. Throughput bị chặn ở
**1 block mỗi (interval + RTT commit)** — chỉ có tối đa **1 block in-flight** tại một thời điểm.

**Cải thiện (cần làm cẩn thận, xem ràng buộc đồng bộ ở Phần 3):**
- Cho phép **nhiều block in-flight** (pipelining): leader tiếp tục propose block kế tiếp dựa trên
  `RaftLog.GetLastIndex()` (đã tăng khi AppendEntry) mà không chờ commit. PrevHash/PrevLogIndex lấy theo
  entry chưa commit gần nhất thay vì block đã commit.
- Hoặc tối thiểu: bỏ `time.After(AutoProposeInterval)` khi pool đã đủ batch size — propose ngay khi
  `len(TxPool) >= batchSize` (event-driven thay vì polling). Dùng interval chỉ như timeout flush khi pool nhỏ.

> Lưu ý: hiện `proposeBlockWithTxs` lấy PrevHash từ `getLastCommittedHash()` ([transaction.go:177](../source/internal/raft/transaction.go#L177))
> nên pipeline sẽ sai hash-chain nếu chưa commit. Pipelining đòi đổi sang lấy PrevHash từ entry cuối trong RaftLog.

---

### 🔴 OPT-3 — Tái sử dụng stream libp2p (loại bỏ tạo stream mới mỗi message)

> ✅ **Đã triển khai cho đường tải nóng (endorsement).** Đây là **cổ chai trần ~5600 tx/s đo được** ở
> test 10k TPS: loadgen mở **1 stream/tx** (32 worker × open/close) → ~10000 stream/s, orderer không
> accept kịp → stream bị reset/drop, ~40% tx **mất âm thầm** (`sent` thành công cục bộ nhưng orderer chưa
> đọc). Triệu chứng: `sent=96971` nhưng `commit` tổng chỉ `57252`, drain không xả (pool không backlog),
> avg chỉ 741 tx/block (pool đói, không bao giờ đầy 1000).
>
> Fix:
> - **Orderer** `HandleEndorsementStream` ([endorsement.go](../source/internal/raft/endorsement.go)) đọc
>   **nhiều tx/stream** trong vòng lặp `for { decoder.Decode(&tx) }` tới khi stream đóng (tương thích
>   ngược sender cũ 1 tx/stream).
> - **Loadgen** mỗi worker giữ **1 stream lâu dài**, `json.Encoder.Encode` mọi tx
>   ([sender.go](../source/pkg/loadgen/sender.go) `openEndorsementStream` + `workerFn`). 10000 stream/s
>   → 32 stream. Backpressure yamux window thay cho drop ⇒ `sent` ≈ thực nhận, không mất tx.
>
> **Chưa làm:** áp dụng tương tự cho `SendMessage`/`BroadcastMessage` (proposal/ack/commit giữa các node)
> — kèm ràng buộc SYNC-2 (đúng thứ tự trên stream lâu dài). Đường này tải thấp hơn endorsement nên ưu tiên sau.

**File/hàm:** `Transport.SendMessage` — [transport.go:97-110](../source/internal/network/transport.go#L97-L110),
`Transport.BroadcastMessage` — [transport.go:130-147](../source/internal/network/transport.go#L130-L147),
`handleStream` (1 message/stream) — [node.go:176-188](../source/internal/raft/node.go#L176-L188)

**Vấn đề:** Mỗi message (heartbeat, proposal, ACK, commit, forward tx...) đều **mở một stream libp2p mới**
(`Host.NewStream`) rồi `defer s.Close()`. Phía nhận decode đúng **một** message rồi đóng stream.
Ở tần suất block cao + nhiều tx forward, chi phí mở/đóng stream (protocol negotiation, multiplexer setup)
trở thành cổ chai mạng lớn nhất.

**Cải thiện:**
- Duy trì **persistent stream pool** mỗi peer (1 stream dài hạn, ghi liên tiếp nhiều message, dùng
  length-prefixed framing hoặc `json.Encoder` trên stream giữ mở). Phía nhận đọc nhiều message trong vòng lặp.
- Thay `handleStream` đọc 1 message → vòng lặp `for { decoder.Decode(&msg) }` cho tới khi stream đóng.
- Cân nhắc thay JSON bằng codec nhị phân (protobuf/msgpack) cho message nóng (proposal/commit/ack).

---

### 🟠 OPT-4 — Loại bỏ marshal/unmarshal hai lần ở dispatcher

**File/hàm:** `handleMessage` — [consensus.go:20-72](../source/internal/raft/consensus.go#L20-L72) và mọi handler
(`HandleBlockProposal`, `HandleBlockProposalAck`, `HandleBlockCommit`, `HandleTxRequest`...).

**Vấn đề:** `msg.Data` được decode thành `interface{}` ở `handleStream`, rồi **mỗi handler lại
`json.Marshal(msg.Data)` + `json.Unmarshal`** để ép về struct cụ thể. Ví dụ
[transaction.go:369-379](../source/internal/raft/transaction.go#L369-L379). Đây là round-trip serialize
thừa trên đường đi nóng nhất (mỗi proposal/ack/commit).

**Cải thiện:** Khai báo `Data json.RawMessage` trong `types.Message` (decode lười), handler chỉ
`Unmarshal(msg.Data, &target)` một lần. Loại bỏ hẳn bước `Marshal` lặp lại.

---

### 🟠 OPT-5 — Tách kênh xử lý message nóng khỏi `MessageChan` chung

**File/hàm:** `MessageChan` (buffer 100) + goroutine `processMessages` đơn — [node.go:133](../source/internal/raft/node.go#L133),
[consensus.go:8-17](../source/internal/raft/consensus.go#L8-L17)

**Vấn đề:** Tất cả message (heartbeat, tx request, proposal, ack, commit, membership, sync) đi qua
**một channel + một goroutine xử lý tuần tự**. Khi TPS tx cao, việc nạp tx (`HandleTxRequest`) cạnh tranh
với xử lý ACK/commit → head-of-line blocking, kéo dài thời gian đóng block. Một handler chậm chặn toàn bộ.

**Cải thiện:**
- Tách kênh riêng cho message liên quan đồng thuận (proposal/ack/commit) ưu tiên xử lý, hoặc
- Worker pool cho `HandleTxRequest` (chỉ append vào TxPool, có thể song song với mutex `TxPoolMu`).

> ✅ **Đã triển khai (một phần).** Trong `handleStream` ([node.go:187-201](../source/internal/raft/node.go#L187-L201)):
> - `MsgBlockProposalAck` bypass `MessageChan` → đẩy thẳng `BlockAckChan` (non-blocking).
> - `MsgTxRequest` bypass `MessageChan` → gọi `HandleTxRequest` inline ngay trên goroutine của stream
>   (mỗi stream đã là 1 goroutine riêng; append `TxPool` được `TxPoolMu` bảo vệ).
>
> Hệ quả: ở TPS cao, `MessageChan` không còn bị tx flood làm đầy → các handleStream goroutine không
> còn block tại `rn.MessageChan <- msg`, giải phóng scheduler/libp2p mux → ACK/commit/heartbeat được
> xử lý kịp trong cửa sổ `waitForBlockAcks` (5s) ngay trong lúc load. Đồng thời tx-ack gửi về client
> đã chuyển sang async ([transaction.go:128-133](../source/internal/raft/transaction.go#L128-L133)) để
> không chặn đường nạp tx.
>
> Còn lại (chưa làm): tách hẳn kênh ưu tiên cho consensus + worker pool có giới hạn cho tx, và OPT-3
> (persistent stream) để cắt chi phí mở/đóng stream mỗi tx ở tầng mạng.
>
> ⚠️ **Đính chính quan trọng:** loadgen gửi tx qua **endorsement protocol** (`SendEndorsement` →
> `HandleEndorsementStream` [endorsement.go](../source/internal/raft/endorsement.go)), **KHÔNG** qua
> `MsgTxRequest`/`MessageChan`. Do đó bypass `MsgTxRequest` ở `handleStream` **không** ảnh hưởng đường tải
> của loadgen (chỉ ảnh hưởng tx forward giữa node). Bottleneck thực đo được nằm ở **OPT-8** bên dưới.

---

### 🟠 OPT-6 — `MerkleRoot` băm trên `Txid` (string) thay vì tái dùng

**File/hàm:** `ComputeMerkleRoot` — [block.go:83-160](../source/internal/types/block.go#L83-L160)

**Quan sát:** Merkle root băm `[]byte(tx.Txid)` (chuỗi hex). Đã có parallel cho block > 1000 tx.
Với `AutoProposeBlockSize=1000`, đa số block **không** chạm nhánh parallel (điều kiện `n > 1000`).

**Cải thiện:** Hạ ngưỡng parallel xuống (vd `n > 256`) hoặc dựa trên `runtime.NumCPU()`; cache `Txid`
dạng `[]byte` để tránh convert lặp. Tác động nhỏ hơn OPT-1..3 nhưng dễ làm.

---

### 🟡 OPT-7 — `waitForBlockAcks` chốt `totalCount`/`majority` lúc bắt đầu

**File/hàm:** [transaction.go:228-229](../source/internal/raft/transaction.go#L228-L229)

**Quan sát:** `majority` tính từ `Membership.GetTotalCount()` tại thời điểm bắt đầu chờ. Nếu membership đổi
giữa chừng (node chết/join), ngưỡng majority có thể lệch → chờ thừa hoặc commit thiếu an toàn.

**Cải thiện:** Tính lại majority động khi đánh giá, hoặc chốt theo snapshot membership đính kèm proposal
để leader và follower nhất quán quorum.

---

### 🔴 OPT-8 — Bỏ logging trên hot-path (cổ chai đo được thực tế ở cmd/server)

> ✅ **Đã triển khai.**

**File/hàm:** `HandleEndorsementStream` [endorsement.go](../source/internal/raft/endorsement.go),
`processTx` [transaction.go:33](../source/internal/raft/transaction.go#L33),
`ExecuteBlockTransactions` [transaction.go](../source/internal/raft/transaction.go)

**Vấn đề (triệu chứng quan sát):** Ở 5000 TPS / 20-30s, ingest chạy bình thường (sent≈99.5k–149.5k,
failed=0) nhưng **commit gần như đứng yên trong lúc load** (chỉ 1 block ~71 tx), rồi **xả ồ ạt
~1200-1600 tx/s ngay khi load dừng**. Ingest không chậm, chỉ commit kẹt ⇒ điểm tranh chấp nằm ở tài
nguyên **ingest và commit dùng chung**.

Tài nguyên đó là **một `log.Logger` duy nhất** (`*log.Logger` serialize mọi `Printf` qua 1 mutex + I/O
console qua readline). Leader log **3 dòng/tx** trên đường nạp:
- `HandleEndorsementStream`: "Received endorsement for tx ..." và "Endorsement tx ... added to TxPool"
- `processTx`: "Received tx ... (pool size: N)"

→ 5000 TPS × 3 = **15000 lượt ghi log/s** giữ mutex logger. Đường commit (`waitForBlockAcks`,
`commitBlock`, `BroadcastMessage`, và `ExecuteBlockTransactions` log **mỗi tx trong block** = 1000
dòng/block) phải lấy **cùng mutex** → bị block sau dòng log tx. Load dừng → áp lực log giảm → commit xả.

**Cải thiện (đã làm):** Xóa toàn bộ log per-tx trên hot-path (3 dòng ingest + vòng lặp log trong
`ExecuteBlockTransactions`). Giữ log ở mức block (Proposing/Committing/Accepted ~1 dòng/block).

**Còn lại / production:** chuyển sang logger bất đồng bộ (ring buffer + 1 goroutine flush) hoặc leveled
logging tắt được ở mức tx; cân nhắc OPT-3 để cắt 5000 stream-connect/s ở tầng libp2p.

---

## Phần 3 — Đảm bảo đồng bộ dữ liệu & trạng thái khi tối ưu

Các tối ưu tốc độ ở trên (đặc biệt OPT-2 pipeline) **không được phá vỡ** tính nhất quán. Dưới đây là các
ràng buộc & lỗ hổng đồng bộ cần xử lý song song.

### 🔴 SYNC-1 — Follower mất `MsgBlockCommit` → tụt hậu âm thầm

**File/hàm:** `HandleBlockCommit` — [transaction.go:493-559](../source/internal/raft/transaction.go#L493-L559),
`HandleBlockProposal` — [transaction.go:363-445](../source/internal/raft/transaction.go#L363-L445)

**Vấn đề:** Follower chỉ append vào `OrderingBlock` khi nhận được `MsgBlockCommit`. Nếu commit message **bị mất**:
- Entry vẫn nằm trong `RaftLog` (đã append lúc proposal) → proposal kế tiếp **vẫn pass** kiểm tra
  `PrevLogIndex` ([transaction.go:410](../source/internal/raft/transaction.go#L410)).
- Cơ chế phát hiện gap hiện tại chỉ kích hoạt khi commit trỏ tới **log entry không tồn tại**
  ([transaction.go:534-539](../source/internal/raft/transaction.go#L534-L539)) — nhưng ở đây entry **tồn tại**.
- ⇒ Follower thiếu một block trong `OrderingBlock` mà **không ai phát hiện** cho tới khi rejoin/stale.
  `lastCommittedHash` của follower cũng đứng yên.

**Cải thiện (bắt buộc nếu tăng tốc độ block):**
- Thêm **kiểm tra commitIndex liên tục**: khi `HandleBlockCommit` thấy `commit.LogIndex >
  OrderingBlock.GetLastIndex()+1` (nhảy cóc) → `go StartSync("commit-gap")`.
- Hoặc follower so `commit.LogIndex` với `OrderingBlock.GetLastIndex()` và tự fetch các block còn thiếu.

### 🔴 SYNC-2 — Pipeline (OPT-2) cần follower xử lý nhiều proposal in-flight đúng thứ tự

**File/hàm:** kiểm tra `entry.PrevLogIndex != lastIndex` — [transaction.go:409-417](../source/internal/raft/transaction.go#L409-L417)

**Vấn đề:** Follower **từ chối** mọi proposal không liền mạch (`PrevLogIndex` phải đúng bằng last index).
Nếu leader pipeline gửi block N và N+1 gần nhau và chúng tới **không đúng thứ tự** (mỗi message một stream,
chạy goroutine riêng — [transport.go:136](../source/internal/network/transport.go#L136)), N+1 sẽ bị reject.

**Cải thiện:**
- Đảm bảo **thứ tự gửi** trên một stream dài hạn theo peer (gắn với OPT-3) → message tới đúng thứ tự.
- Hoặc follower **buffer** proposal đến sớm (out-of-order) và áp dụng khi liền mạch.

### 🟠 SYNC-3 — Message đồng thuận bị **drop** khi đang sync

**File/hàm:** `handleMessage` — [consensus.go:21-28](../source/internal/raft/consensus.go#L21-L28)

**Vấn đề:** Trong lúc `IsSyncing()`, `MsgBlockProposal`/`MsgBlockCommit` bị **bỏ hẳn** (return). Nếu sync target
được chốt trước khi các block mới này commit, follower có thể bỏ lỡ block nằm ngoài phạm vi sync vừa kéo.
Hiện dựa vào lần commit kế tiếp để re-trigger sync (kết hợp SYNC-1 đang hở).

**Cải thiện:** Sau khi `exitSync`, chủ động so `commitIndex` với leader (qua heartbeat/commit kế) và sync bù
phần phát sinh trong cửa sổ sync; hoặc dồn (queue) proposal/commit thay vì drop.

### 🟠 SYNC-4 — Không có persistence: tăng tốc độ ⇒ tăng dữ liệu mất khi crash

**File/hàm:** `OrderingBlock`/`RaftLog` in-memory — [block.go:193-277](../source/internal/types/block.go#L193-L277)

**Vấn đề:** Khi đẩy throughput cao hơn, lượng block/log chưa được nhân bản đủ rộng tại một thời điểm tăng,
rủi ro mất dữ liệu khi node (đặc biệt leader) crash cao hơn. Hiện restart = mất sạch state, chỉ phục hồi qua sync.

**Cải thiện:** Thêm WAL/persistence cho `RaftLog` + `OrderingBlock` (append-only file / BoltDB), fsync theo batch
để cân bằng độ trễ. Đây là điều kiện cần để các tối ưu tốc độ an toàn ở mức production.

### 🟡 SYNC-5 — Hash-chain chỉ verify khi sync, không khi nhận proposal/commit thường

**File/hàm:** `verifyHashChain` chỉ gọi trong `StartSync` — [sync.go:84](../source/internal/raft/sync.go#L84);
`HandleBlockProposal`/`HandleBlockCommit` **không** verify `block.PrevHash` so với `lastCommittedHash`.

**Vấn đề:** Đường đi nóng (proposal/commit) tin tưởng leader, chỉ kiểm tra `PrevLogIndex`/term, **không**
kiểm tra liên tục hash-chain của block committed. Khi pipeline/đa luồng, sai lệch hash-chain có thể lọt
tới khi sync mới phát hiện.

**Cải thiện:** Khi follower commit (`HandleBlockCommit`), verify `entry.Block.PrevHash == lastCommittedHash`
trước khi `AppendBlock`. Rẻ và chặn sớm phân nhánh.

---

## Phần 4 — Lộ trình đề xuất (ưu tiên)

| Bước | Hạng mục | Lợi ích | Rủi ro đồng bộ phải kèm |
|---|---|---|---|
| 0 | **OPT-8** bỏ logging hot-path ✅ | gỡ cổ chai thực tế ở 5000 TPS (commit kẹt → xả khi load dừng) | không |
| 1 | **OPT-1** commit ngay khi đủ majority ✅ | -tới 100ms/block, gần như free | không |
| 2 | **OPT-4** bỏ marshal 2 lần | giảm CPU đường nóng | không |
| 3 | **OPT-3** persistent stream + đọc nhiều msg/stream (endorsement ✅) | gỡ trần ingest ~5600 tx/s | nội node: cần SYNC-2 |
| 4 | **OPT-5** tách kênh message nóng ✅ (bypass tx+ack khỏi MessageChan) | bỏ head-of-line blocking | không |
| 5 | **SYNC-1 + SYNC-5** phát hiện gap commit + verify hash-chain | an toàn nền tảng | (bắt buộc trước bước 6) |
| 6a | **OPT-2** event-driven (1 in-flight, bỏ interval tax) ✅ | bỏ trần ~7 blocks/s do interval | không |
| 6b | **OPT-2** pipeline thật (nhiều in-flight) | tăng throughput nhiều lần | cần SYNC-1, SYNC-2, SYNC-5 |
| 7 | **SYNC-4** persistence (WAL) | an toàn production | nền tảng dài hạn |
| 8 | **OPT-6 / OPT-7** merkle parallel ngưỡng thấp, majority động | tinh chỉnh | không |

**Nguyên tắc:** mỗi bước tăng tốc (OPT) chỉ triển khai sau khi cơ chế phát hiện gap/verify tương ứng (SYNC)
đã sẵn sàng, để không đánh đổi tính nhất quán lấy tốc độ.

---

## Phụ lục — Bảng tham chiếu nhanh file/hàm

| Chủ đề | File | Hàm chính |
|---|---|---|
| Propose/commit block | [transaction.go](../source/internal/raft/transaction.go) | `ProposeBlock`, `proposeBlockWithTxs`, `waitForBlockAcks`, `commitBlock` |
| Auto-propose loop | [transaction.go](../source/internal/raft/transaction.go) | `StartAutoProposeBlock` |
| Follower nhận block | [transaction.go](../source/internal/raft/transaction.go) | `HandleBlockProposal`, `HandleBlockCommit` |
| Dispatcher message | [consensus.go](../source/internal/raft/consensus.go) | `handleMessage`, `processMessages` |
| Mạng / stream | [transport.go](../source/internal/network/transport.go) | `SendMessage`, `BroadcastMessage`, `OpenSyncStream` |
| Stream nhận | [node.go](../source/internal/raft/node.go) | `handleStream` |
| Lưu trữ log/block | [block.go](../source/internal/types/block.go) | `RaftLog`, `OrderingBlock`, `ComputeMerkleRoot` |
| Sync (pull) | [sync.go](../source/internal/raft/sync.go) | `StartSync`, `pickSyncTarget`, `fetchBlocksParallel`, `verifyHashChain` |
| Sync server | [sync_server.go](../source/internal/raft/sync_server.go) | `handleSyncStatusRequest`, `HandleSyncStream`, `streamBlocks` |
| Heartbeat | [heartbeat.go](../source/internal/raft/heartbeat.go) | `checkHeartbeat`, `sendHeartbeat`, `handleHeartbeat` |
| Config runtime | [config.go](../source/internal/raft/config.go) | `Config`, getters/setters |
</content>
</invoke>
