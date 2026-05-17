# Phân tích quá trình bầu chọn Leader (Leader Election)

> **Phạm vi:** Ordering Service — triển khai Raft tùy chỉnh dựa trên libp2p.
> **Files liên quan:** [leader.go](../source/internal/raft/leader.go), [heartbeat.go](../source/internal/raft/heartbeat.go), [node.go](../source/internal/raft/node.go), [membership.go (raft)](../source/internal/raft/membership.go), [membership.go (types)](../source/internal/types/membership.go)

---

## 1. Tổng quan cơ chế

Hệ thống sử dụng **Raft biến thể dựa trên Priority** thay vì Raft chuẩn dùng bỏ phiếu ngẫu nhiên. Ý tưởng cốt lõi: node tham gia cluster sớm nhất được gán **priority thấp hơn (giá trị số nhỏ hơn) = ưu tiên cao hơn**; khi leader sập, node có priority cao nhất còn sống sẽ tự động trở thành leader mới.

### 1.1 Các trạng thái node

```
Follower  ←──────────────────────────────────────────┐
    │                                                  │
    │ Heartbeat timeout (5s)                           │
    ▼                                                  │
selectNewLeader()                                      │
    │                                                  │
    ├─── [Tôi là node priority cao nhất?]              │
    │         YES                                       │
    │         ▼                                         │
    │    ClaimingLeader                                 │
    │    (gửi MsgIAmNewLeader, chờ majority ACK)        │
    │         │                                         │
    │    [YES ≥ majority?]                              │
    │         │YES                      NO─────────────┤
    │         ▼                                         │
    │       Leader ─────── step down ──────────────────┘
    │
    └─── [NO] → đặt expectedLeaderID, chờ 15s
```

### 1.2 Sơ đồ message trong một lần bầu chọn

```
Leader cũ sập
    │
    ├── Node B (priority cao nhất alive)
    │       │
    │       ├─ Tăng term, chuyển sang ClaimingLeader
    │       ├─ Broadcast MsgIAmNewLeader → [tất cả members kể cả dead]
    │       └─ Chờ MsgLeaderClaimAck trong 10 giây
    │
    ├── Node C (follower thường)
    │       │
    │       ├─ Nhận MsgIAmNewLeader từ B
    │       ├─ Kiểm tra B có phải highest-priority? → YES
    │       └─ Gửi MsgLeaderClaimAck{Accept: true}
    │
    └── Node D (follower thường)
            │
            ├─ Nhận MsgIAmNewLeader từ B
            └─ Gửi MsgLeaderClaimAck{Accept: true}

B nhận đủ majority ACK → becomeLeader()
    │
    ├─ Bắt đầu gửi MsgHeartbeat
    └─ Bắt đầu StartAutoProposeBlock()
```

### 1.3 Các hằng số thời gian liên quan

| Hằng số | Giá trị | Vai trò |
|---|---|---|
| `HeartbeatTimeout` | 5 giây | Follower bắt đầu bầu chọn sau khi không nhận heartbeat |
| `HeartbeatInterval` | 2 giây | Leader gửi heartbeat nếu không có block message gần đây |
| Claim ACK window | 10 giây (2×HBT) | `waitForLeaderClaimAcks` — thời gian chờ nhận ACK |
| Expected leader deadline | 15 giây (3×HBT) | Follower chờ expected leader gửi `MsgIAmNewLeader` |
| Monitor ticker | 1 giây | Tần suất `checkHeartbeat()` kiểm tra timeout |

### 1.4 Tính majority (quorum)

```go
// leader.go:103
yesCount := 1              // bản thân coi như YES
totalCount := rn.Membership.GetTotalCount()  // alive + dead
majority := totalCount/2 + 1
```

Quorum tính trên **tổng số member** (kể cả dead) để chống split-brain: nếu cluster bị chia làm hai nhóm bằng nhau (partition 2/2), không nhóm nào đủ majority → leader cũ được giữ nguyên.

---

## 2. Luồng chi tiết theo từng hàm

### 2.1 Phát hiện timeout — `checkHeartbeat()`

[heartbeat.go:30](../source/internal/raft/heartbeat.go#L30)

- **Leader**: gửi heartbeat nếu `time.Since(lastBlockSentTime) >= 2s`.
- **ClaimingLeader**: bỏ qua hoàn toàn (tránh gọi lại `selectNewLeader`).
- **Follower**:
  - Nếu đang chờ expected leader và `expectedLeaderDeadline` đã hết → đánh dấu expected leader chết, gọi `selectNewLeader()` lại.
  - Nếu `time.Since(lastHeartbeat) > 5s && leaderID != ""` → gọi `selectNewLeader()`.

### 2.2 Khởi động bầu chọn — `selectNewLeader()`

[leader.go:17](../source/internal/raft/leader.go#L17)

1. Đánh dấu `currentLeaderID` là dead.
2. Lấy `highestPriority = GetHighestPriorityAliveNode()`.
3. Nếu `highestPriority.PeerID == self` → gọi `sendIAmNewLeaderAndWaitForAcks()`.
4. Nếu không → đặt `currentLeaderID = ""`, `expectedLeaderID = highestPriority.PeerID`, `expectedLeaderDeadline = now + 15s`. **Không tăng `currentTerm`** — term chỉ tăng khi node thật sự gửi claim.

### 2.3 Gửi claim — `sendIAmNewLeaderAndWaitForAcks()`

[leader.go:66](../source/internal/raft/leader.go#L66)

1. Chuyển state sang `ClaimingLeader`, tăng `currentTerm`.
2. Broadcast `MsgIAmNewLeader` tới **tất cả** members (kể cả dead).
3. Spawn goroutine `waitForLeaderClaimAcks(newTerm)`.

### 2.4 Nhận phiếu — `handleIAmNewLeader()`

[leader.go:156](../source/internal/raft/leader.go#L156)

- Nếu đang chờ expected leader: chỉ chấp nhận từ đúng expected node.
- Nếu không: spawn goroutine `evaluateAndAckLeaderClaim()`.

### 2.5 Đánh giá claim — `evaluateAndAckLeaderClaim()`

[leader.go:193](../source/internal/raft/leader.go#L193)

- Nếu claimer không phải highest-priority, tính `remaining = lastHB + HeartbeatTimeout - now()`.
- Nếu `remaining > 0` → `time.Sleep(remaining)` rồi đánh giá lại.
- Điều kiện chấp nhận: `hp.PeerID == claimerID && data.NewTerm >= curTerm`.

### 2.6 Kết thúc — `finishClaim()`

[leader.go:133](../source/internal/raft/leader.go#L133)

- Nếu `yesCount >= majority` → `state = Leader`, bắt đầu gửi heartbeat và auto-propose block.
- Nếu không → quay về `Follower`.

---

## 3. Các điểm thiếu sót cần cải thiện

Các vấn đề được phân loại theo mức độ ảnh hưởng.

---

### 🔴 Mức độ NGHIÊM TRỌNG (ảnh hưởng đến tính đúng đắn)

#### [T1] Không có persistent state — vi phạm nguyên tắc cơ bản của Raft

**Vấn đề:** Raft chuẩn yêu cầu 3 giá trị phải được ghi vào stable storage trước khi phản hồi bất kỳ RPC nào: `currentTerm`, `votedFor`, và `log[]`. Hiện tại toàn bộ state lưu in-memory trong `RaftNode`.

**Hệ quả khi node crash và restart:**
- Node khởi động lại với `currentTerm = 0` — có thể bỏ phiếu cho leader cũ trong term đã kết thúc.
- `lastCommittedHash` bị mất — nếu node lên leader, nó sẽ dùng hash sai làm `PrevHash` cho block mới, phá vỡ chuỗi hash.
- Nếu toàn bộ cluster restart: không node nào biết mình đã commit gì trước đó.

**Vị trí ảnh hưởng:** [node.go:95–113](../source/internal/raft/node.go#L95), [leader.go:74](../source/internal/raft/leader.go#L74)

**Giải pháp đề xuất:** Ghi `currentTerm`, `lastCommittedHash`, `commitIndex` vào file hoặc database (BoltDB, badger) trước khi trả lời bất kỳ message nào. Khi khởi động, đọc lại state đã lưu.

---

#### [T2] Log safety không được kiểm tra trong bầu chọn

**Vấn đề:** Trong Raft chuẩn, một candidate chỉ được bầu làm leader nếu log của nó ít nhất bằng (up-to-date) so với majority. Ở đây, điều kiện duy nhất để trở thành leader là có **priority cao nhất** (join sớm nhất).

**Tình huống lỗi:**
```
- Node A (priority 0, leader) commit block B100, crash ngay sau đó.
- Node B (priority 1) chưa nhận block B100 kịp.
- Node B thắng bầu chọn vì priority cao nhất còn alive.
- Node B bắt đầu propose block B101 với PrevHash của B99.
- Chuỗi hash bị fork: C và D có B100, còn B lại bỏ qua.
```

**Vị trí ảnh hưởng:** [leader.go:17–62](../source/internal/raft/leader.go#L17)

**Giải pháp đề xuất:** Trong `handleIAmNewLeader`, follower chỉ bỏ phiếu YES nếu `data.LastCommitIndex >= local.commitIndex`. Đồng thời claimer cần broadcast `lastCommitIndex` trong message `MsgIAmNewLeader`.

---

#### [T3] Auto-propose block bắt đầu trước khi sync hoàn tất

**Vấn đề:** Trong `finishClaim()` (sau khi thắng bầu chọn), cả `sendHeartbeat()` lẫn `StartAutoProposeBlock()` đều được spawn ngay lập tức:

```go
// leader.go:144–145
go rn.sendHeartbeat()
go func() { _ = rn.StartAutoProposeBlock(AutoProposeBlockSize) }()
```

Nếu node thắng bầu chọn nhưng bị thiếu một số block (vì partition trước đó), nó sẽ propose block mới với `lastCommittedHash` cũ → block mới sẽ có `PrevHash` sai.

Sync (`StartSync`) chỉ được trigger khi nhận heartbeat sau gap dài (`rejoin-after-disconnect`) — không phải ngay sau khi trở thành leader.

**Vị trí ảnh hưởng:** [leader.go:133–151](../source/internal/raft/leader.go#L133), [transaction.go](../source/internal/raft/transaction.go)

**Giải pháp đề xuất:** Trước khi bắt đầu `StartAutoProposeBlock`, leader mới cần:
1. Chạy `StartSync("new-leader")` để đảm bảo đã có đủ committed blocks.
2. Chỉ bắt đầu propose sau khi sync hoàn tất.

---

#### [T4] Priority assignment không nhất quán khi nhiều node join đồng thời

**Vấn đề:** Priority được tính bằng `len(mv.Members)` tại thời điểm `AddMember()` được gọi:

```go
// types/membership.go:42
Priority: len(mv.Members), // Priority based on join order
```

Leader là nguồn duy nhất xử lý join request và broadcast membership. Tuy nhiên, nếu hai node join gần như đồng thời:
- Leader xử lý C trước D → C nhận priority 2, D nhận priority 3.
- Nhưng do message delay, một số follower có thể nhận membership update của D trước C.
- Sau khi nhận cả hai, thứ tự phụ thuộc vào `updateMembershipFromData` — nó **overwrite** thay vì merge.

**Vị trí ảnh hưởng:** [types/membership.go:32–44](../source/internal/types/membership.go#L32), [membership.go:292–304](../source/internal/raft/membership.go#L292)

**Hệ quả:** Hai node có thể đồng thời nghĩ chúng là highest-priority → gửi `MsgIAmNewLeader` cùng lúc → bầu chọn thất bại, không ai đạt majority.

**Giải pháp đề xuất:** Dùng `joinTime` (đã có trong `MemberInfo`) để tính priority thay vì `len(Members)`. Sắp xếp theo `joinTime` → priority = thứ tự trong danh sách đã sort. Cách này idempotent và deterministic bất kể thứ tự message.

---

### 🟡 Mức độ QUAN TRỌNG (ảnh hưởng đến độ tin cậy)

#### [Q1] Khoảng thời gian chờ không nhất quán (15s vs 10s)

**Vấn đề:** Follower không phải highest-priority đặt `expectedLeaderDeadline = now + 15s` để chờ expected leader gửi `MsgIAmNewLeader`. Nhưng expected leader chỉ chờ ACK trong **10 giây** (2×HeartbeatTimeout).

```
Follower chờ expected leader: 15 giây
Expected leader chờ ACK:      10 giây  ← kết thúc sớm hơn 5 giây
```

**Hệ quả:** Trong 5 giây còn lại của expected deadline, follower vẫn đang chờ lệnh `MsgIAmNewLeader` từ expected leader — nhưng leader đó đã bỏ cuộc và quay về Follower. Không ai trigger bầu chọn mới cho đến khi deadline 15 giây hết.

**Vị trí ảnh hưởng:** [leader.go:59](../source/internal/raft/leader.go#L59), [leader.go:105](../source/internal/raft/leader.go#L105)

**Giải pháp:** Đồng bộ hai hằng số — `expectedLeaderDeadline = now + 2×HeartbeatTimeout + margin`.

---

#### [Q2] Race condition trong `evaluateAndAckLeaderClaim` — `time.Sleep` blocking

**Vấn đề:** Hàm này có thể sleep tới 5 giây (`time.Sleep(remaining)`). Trong khoảng thời gian đó, state của node có thể thay đổi hoàn toàn — leader mới đã được bầu, term mới đã tăng. Khi goroutine thức dậy, nó đọc lại state nhưng không kiểm tra `expectedLeaderID`:

```go
// leader.go:207–225
time.Sleep(remaining)
// Sau đây không kiểm tra liệu cluster đã có leader mới chưa
hp = rn.Membership.GetHighestPriorityAliveNode()
rn.mu.RLock()
curTerm = rn.currentTerm
rn.mu.RUnlock()

if hp != nil && hp.PeerID == claimerID && data.NewTerm >= curTerm {
    accept = true  // ← Có thể accept claim đã stale
```

**Hệ quả:** Node có thể gửi YES cho một claim đã hết hạn, dẫn đến hai leader cùng nghĩ mình có majority.

**Vị trí ảnh hưởng:** [leader.go:193–226](../source/internal/raft/leader.go#L193)

**Giải pháp:** Thêm kiểm tra `currentLeaderID` sau khi sleep — nếu đã có leader được công nhận thì không gửi YES.

---

#### [Q3] `currentLeaderID` bị set sớm trong non-highest-priority node

> ✅ **Đã xử lý.** Fix trong `selectNewLeader()` nhánh ELSE: đặt `currentLeaderID = ""` thay vì `= highestPriority.PeerID`, đồng thời xóa `currentTerm++`. Chi tiết: [TC02](scenarios/tc02-f2-timeout.md#7-lịch-sử-fix).

~~**Vấn đề:** Trong `selectNewLeader()`, khi node không phải highest-priority, `currentLeaderID` được đặt sớm trước khi bầu chọn xong. Điều này gây lan truyền thông tin sai qua `HeartbeatResponse` và có thể khiến leader cũ step down không đúng.~~

**Vị trí đã fix:** [leader.go:49–62](../source/internal/raft/leader.go#L49)

---

#### [Q4] Không có retry cơ chế cho join request khi đang bầu chọn

**Vấn đề:** Node mới chỉ xử lý join request nếu là Leader:

```go
// membership.go:40–43
if !rn.IsLeader() {
    log.Printf("[%s] Not leader, ignoring join request", ...)
    return
}
```

Nếu node mới join đúng lúc cluster đang trong bầu chọn (không có Leader), join request bị drop im lặng. Không có retry hay queue.

**Vị trí ảnh hưởng:** [membership.go:40](../source/internal/raft/membership.go#L40)

**Giải pháp:** Trả lời lỗi rõ ràng hoặc redirect về expectedLeaderID; client tự retry sau một khoảng thời gian.

---

#### [Q5] `handleMembershipAck` — điều kiện step-down quá chặt

**Vấn đề:**

```go
// membership.go:112–118
if wasLeader && leaderID != rn.Transport.ID() {
    highestPriority := rn.Membership.GetHighestPriorityAliveNode()
    if highestPriority != nil && highestPriority.PeerID == leaderID {
        rn.state = types.Follower  // chỉ step down nếu ack đến từ HIGHEST priority
    }
}
```

Nếu highest-priority node không có mặt và một mid-priority node thắng bầu chọn hợp lệ, node leader cũ sẽ không step down khi nhận membership ack từ node đó.

**Vị trí ảnh hưởng:** [membership.go:112](../source/internal/raft/membership.go#L112)

**Giải pháp:** Step down nếu `leaderID != self` **và** `msg.Term >= currentTerm`, không cần kiểm tra priority của `leaderID`.

---

### 🟢 Mức độ NHỎ (ảnh hưởng đến bảo mật và khả năng mở rộng)

#### [N1] Không có xác thực (authentication) trên leadership claim

**Vấn đề:** Bất kỳ node nào cũng có thể gửi `MsgIAmNewLeader` với `NewLeaderID` tùy ý. Không có chữ ký hay xác thực rằng sender IS node mà nó khai.

**Hệ quả:** Node độc hại có thể giả mạo là highest-priority node và cướp leadership nếu không bị phát hiện bởi priority check.

**Lưu ý:** Priority check giảm nhẹ rủi ro nhưng không loại bỏ hoàn toàn trong môi trường adversarial.

**Giải pháp đề xuất:** Ký `MsgIAmNewLeader` bằng private key của node. Receiver verify chữ ký trước khi xử lý. libp2p cung cấp key pair sẵn qua `host.Peerstore()`.

---

#### [N2] `LeaderClaimAckChan` có thể drop message

**Vấn đề:**

```go
// leader.go:267–270
case rn.LeaderClaimAckChan <- msg:
default:
    log.Printf("[...] Leader claim ack channel full, dropping ack from %s", ...)
```

Channel có capacity 100 nhưng vẫn có thể đầy nếu nhiều ACK đến nhanh. ACK bị drop có thể khiến claimer không đạt majority dù thực tế đã có đủ phiếu.

**Vị trí ảnh hưởng:** [leader.go:266](../source/internal/raft/leader.go#L266), [node.go:107](../source/internal/raft/node.go#L107)

**Giải pháp:** Tăng capacity hoặc dùng unbuffered channel với goroutine collector riêng.

---

#### [N3] Không có persistent membership

**Vấn đề:** Nếu toàn bộ cluster restart (planned maintenance hoặc disaster recovery), không node nào biết danh sách members cũ. Mỗi node khởi động lại với `len(Members) == 1` và tự trở thành leader.

**Hệ quả:** Cần cấu hình lại cluster thủ công sau mỗi lần restart toàn bộ.

**Giải pháp đề xuất:** Ghi membership view vào file JSON/BoltDB sau mỗi lần cập nhật. Khi startup, đọc lại file để khôi phục danh sách member đã biết.

---

## 4. Bảng tổng hợp

| ID | Vấn đề | Mức độ | File chính | Trạng thái |
|---|---|---|---|---|
| T1 | Không có persistent state (term, hash) | 🔴 Nghiêm trọng | node.go, leader.go | Chưa xử lý |
| T2 | Log safety không kiểm tra trong bầu chọn | 🔴 Nghiêm trọng | leader.go | Chưa xử lý |
| T3 | Auto-propose trước khi sync hoàn tất | 🔴 Nghiêm trọng | leader.go, transaction.go | Chưa xử lý |
| T4 | Priority không nhất quán khi join đồng thời | 🔴 Nghiêm trọng | types/membership.go | Chưa xử lý |
| Q1 | Khoảng chờ expected leader (15s) vs ACK window (10s) lệch nhau | 🟡 Quan trọng | leader.go | Chưa xử lý |
| Q2 | Race condition sau `time.Sleep` trong `evaluateAndAckLeaderClaim` | 🟡 Quan trọng | leader.go | Chưa xử lý |
| Q3 | `currentLeaderID` set sớm trước khi bầu chọn xong | 🟡 Quan trọng | leader.go, heartbeat.go | ✅ Đã xử lý ([TC02](scenarios/tc02-f2-timeout.md#7-lịch-sử-fix)) |
| Q4 | Không có retry cho join request khi đang bầu chọn | 🟡 Quan trọng | membership.go | Chưa xử lý |
| Q5 | Điều kiện step-down trong `handleMembershipAck` quá chặt | 🟡 Quan trọng | membership.go | Chưa xử lý |
| N1 | Không xác thực chữ ký trên leadership claim | 🟢 Nhỏ | leader.go | Chưa xử lý |
| N2 | `LeaderClaimAckChan` có thể drop ACK | 🟢 Nhỏ | leader.go, node.go | Chưa xử lý |
| N3 | Không có persistent membership | 🟢 Nhỏ | membership.go | Chưa xử lý |

---

## 5. So sánh với Raft chuẩn (Ongaro & Ousterhout, 2014)

| Đặc điểm | Raft chuẩn | Triển khai hiện tại |
|---|---|---|
| Cơ chế bầu chọn | Random timeout + majority vote | Priority-based + majority ACK |
| Đảm bảo log safety | Candidate phải có log up-to-date | Không kiểm tra |
| Persistent state | `currentTerm`, `votedFor`, `log[]` bắt buộc ghi disk | Toàn bộ in-memory |
| Membership change | Joint consensus hoặc single-server changes | Broadcast từ leader, không có 2-phase |
| Điều kiện bỏ phiếu YES | Term cao hơn + log up-to-date | Term >= curTerm + claimer là highest-priority |
| Chống split vote | Randomized timeout | Deterministic priority (tránh split vote tốt) |
| Xác thực message | Không quy định | Không có |

---

## 6. Khuyến nghị ưu tiên

**Ưu tiên 1 — Trước khi production:**

1. **[T3]** Đảm bảo leader mới chạy sync trước khi propose block.
2. **[T4]** Dùng `joinTime` để tính priority thay vì `len(Members)`.
3. **[Q1]** Đồng bộ `expectedLeaderDeadline` với `waitForLeaderClaimAcks` timeout.
4. **[Q2]** Thêm guard state check sau `time.Sleep` trong `evaluateAndAckLeaderClaim`.

**Ưu tiên 2 — Cải thiện dài hạn:**

5. **[T1]** Thêm persistent storage cho `currentTerm` và `lastCommittedHash`.
6. **[T2]** Kiểm tra `lastCommitIndex` trong leadership claim.
7. **[N3]** Persist membership view.

**Ưu tiên 3 — Tùy chọn:**

9. **[N1]** Ký và verify leadership claim bằng libp2p key pair.
10. **[Q4]** Retry mechanism cho join request.
11. **[Q5]** Sửa điều kiện step-down.
12. **[N2]** Tăng ACK channel capacity.
