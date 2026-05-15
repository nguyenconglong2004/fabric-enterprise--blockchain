# Raft Order Service - Hướng dẫn sử dụng

## Giới thiệu

Implementation của Raft consensus protocol với priority-based leader succession. Service nhận transactions từ client (hoặc Core Service), đóng gói thành blocks, và phân phối đến committing peers.

Đặc điểm chính:
- **Priority-based leader succession**: thay vì bỏ phiếu ngẫu nhiên, leader mới được xác định theo `JoinTime` (ai vào cluster sớm hơn → priority thấp hơn → ưu tiên cao hơn).
- **Block proposal / commit pipeline**: leader gom tx → tạo `Block` (Merkle root + hash chain) → broadcast `MsgBlockProposal` → chờ majority `MsgBlockProposalAck` → broadcast `MsgBlockCommit`.
- **Transaction**: UTXO model bitcoin-like, ký Ed25519, scriptPubKey P2PKH.
- **Deliver service**: committing peer subscribe long-lived stream để nhận block đã commit.
- **Data sync**: node mới join hoặc rejoin sau disconnect tự động fetch song song blocks + RaftLog từ peers, verify hash chain trước khi install.

## Cấu trúc Project

```
source/
├── cmd/
│   ├── server/main.go        # Server node (interactive CLI)
│   └── client/main.go        # External UTXO client
├── internal/
│   ├── api/                  # HTTP API server (leader, membership, submit-tx, endorsement)
│   ├── raft/                 # Core Raft logic
│   │   ├── node.go           # RaftNode struct + lifecycle
│   │   ├── consensus.go      # Message dispatcher
│   │   ├── heartbeat.go      # Heartbeat send/check + rejoin detection
│   │   ├── leader.go         # selectNewLeader, IAmNewLeader, claim ACK
│   │   ├── membership.go     # Membership update broadcast / handle
│   │   ├── transaction.go    # TxPool + propose/commit block + auto-propose
│   │   ├── deliver.go        # DeliverManager + HandleDeliverStream
│   │   ├── endorsement.go    # HandleEndorsementStream
│   │   ├── sync.go           # Sync coordinator (client side)
│   │   └── sync_server.go    # Sync handler (server side)
│   ├── network/              # libp2p transport + protocol IDs
│   └── types/                # Data structures (Block, Transaction, Message, Sync...)
├── pkg/
│   └── client/               # Public client API (OrderClient)
├── examples/                 # Ví dụ build/sign/verify transaction
├── docs/                     # MEMBERSHIP_VIEW.md, TRANSACTION.md, CLIENT_CLI.md
└── testingscenarios/         # Test scenario descriptions
```

## Build

```bash
cd source

# Build server
go build -o server ./cmd/server

# Build client
go build -o client ./cmd/client

# Hoặc build cả hai
go build ./...
```

## Khởi động Cluster

### Bước 1: Khởi động Node đầu tiên (Leader)

```bash
./server
```

Nhập thông tin:
```
Enter port for P2P network (e.g., 6000): 6000
Is this the first node? (y/n): y
```

Node đầu tiên sẽ tự động trở thành **Leader**. Ghi lại địa chỉ hiển thị:
```
Address: /ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...
```

### Bước 2: Khởi động Node thứ hai (Follower)

Mở terminal mới:
```bash
./server
```

Nhập thông tin:
```
Enter port for P2P network (e.g., 6000): 6001
Is this the first node? (y/n): n
Enter address of existing node to connect to: /ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...
```

### Bước 3: Khởi động thêm Node (tùy chọn)

Lặp lại bước 2 với port khác (6002, 6003, ...).

## Sử dụng Server Commands

Sau khi server khởi động, bạn có thể sử dụng các lệnh sau:

| Command | Mô tả |
|---------|-------|
| `status` | Hiển thị trạng thái node (ID, State, Term, Leader, Members, TxPool, Raft log, Ordering blocks) |
| `delay <secs> <p1> [p2]...` | Delay heartbeat đến các node có priority chỉ định X giây (chỉ Leader; dùng để test) |
| `connect <addr>` | Kết nối đến node khác |
| `help` | Hiển thị danh sách commands |
| `quit` | Thoát |

### Ví dụ workflow trên Leader:

```bash
> status                    # Kiểm tra trạng thái
> delay 10 1 2              # Delay heartbeat 10s đến node priority 1 và 2 (test isolation)
```

## Sử dụng Client

Client cho phép submit UTXO transactions từ bên ngoài cluster với Ed25519 keypair.

### Khởi động Client

```bash
./client
```

Nhập địa chỉ của một node trong cluster:
```
Enter address of a node in the cluster (e.g., /ip4/127.0.0.1/tcp/6000/p2p/...): /ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...
```

### Client Commands

| Command | Mô tả |
|---------|-------|
| `keygen` | Tạo Ed25519 keypair mới |
| `wallet <seed_hex>` | Load keypair từ seed hex có sẵn |
| `addr` | Hiển thị địa chỉ ví hiện tại |
| `fund <amount>` | Tạo genesis UTXO (coinbase) cho địa chỉ hiện tại (local only, không submit lên mạng) |
| `utxos` | Liệt kê UTXOs có sẵn (auto-sync từ peer nếu đã đăng ký) |
| `sync <peer_addr>` | Đăng ký committing peer để auto-sync UTXOs; đồng bộ ngay lần đầu |
| `tx <to_addr> <amt>` | Tạo và submit signed Ed25519 transaction (auto-sync trước khi gửi) |
| `start [tps]` | Bật auto-send signed transactions (mặc định 1 TPS) |
| `stop` | Tắt auto-send |
| `speed <tps>` | Thay đổi TPS trong khi auto-send đang chạy |
| `status` | Hiển thị thống kê auto-send (state, wallet, sent, acked) |
| `help` | Hiển thị danh sách commands |
| `quit` | Thoát |

### Ví dụ workflow Client:

```bash
# Tạo ví mới
> keygen
New keypair generated.
  Seed (hex): a1b2c3...
  Address:    deadbeef...

# Nạp tiền vào ví (genesis UTXO, chỉ local)
> fund 100000
Genesis UTXO created (not submitted to network).

# Gửi transaction thủ công
> tx deadbeef... 500
Transaction submitted: <txid>
  Inputs: 1  Outputs: 2

# Bật auto-send 10 TPS
> start 10
Auto-send started at 10.00 TPS (signed Ed25519 transactions).

# Xem thống kê
> status
Auto-send: RUNNING | Wallet: deadbeef... | TX counter: 120 | Sent: 118 | Acked: 115

# Đổi tốc độ
> speed 50

# Dừng
> stop
Auto-send stopped. Sent: 300  Acked: 298
```

### Sync UTXOs từ Committing Peer:

```bash
> sync /ip4/127.0.0.1/tcp/7000/p2p/12D3KooW...
Syncing UTXOs for address deadbeef......
Sync complete: +3 new UTXO(s) from blockchain, wallet total = 3 UTXO(s), balance = 299999
Peer registered — utxos/tx will auto-sync from now on.
```

## Test Leader Failover

1. Khởi động 3 nodes (port 6000, 6001, 6002)
2. Node 6000 là Leader (priority 0)
3. Tắt node 6000 (Ctrl+C)
4. Sau 5 giây (`HeartbeatTimeout`), node 6001 (priority 1) phát hiện timeout, gửi `MsgIAmNewLeader`
5. Node 6002 gửi `MsgLeaderClaimAck`; node 6001 trở thành Leader mới
6. Kiểm tra bằng lệnh `status` trên các node còn lại

## Đồng bộ dữ liệu khi Rejoin / First-Join

Khi một node mới tham gia cluster lần đầu, hoặc một node bị mất kết nối rồi quay lại, nó cần lấy lại đầy đủ committed blocks và uncommitted RaftLog entries. Cơ chế **pull-based parallel sync** chạy trong state mới `Syncing`.

### Trigger

Sync được tự động kích hoạt ở 4 điểm:

| Trigger | Điều kiện |
|---|---|
| `first-join` | Sau khi nhận `MsgMembershipAck`, nếu `OrderingBlock` rỗng và cluster có ≥ 2 alive member. |
| `rejoin-after-disconnect` | Trong `handleHeartbeat()`, nếu `time.Since(lastHeartbeat) > 2 × HeartbeatTimeout` (vừa có gap dài). |
| `stepped-down-after-stale-heartbeat` | Stale leader nhận `MsgHeartbeatResponse` cho thấy có term mới hơn → step down rồi sync. |
| `missing-log-entry` | Nhận `MsgBlockCommit` mà `RaftLog.FindEntryByIndex` trả `nil` → self-heal. |

### Quy trình sync (3 phase)

1. **Discovery** — node sync broadcast `MsgSyncStatusRequest` tới mọi alive peer; thu `MsgSyncStatusResponse` chứa `(term, commitIndex, commitHash, logLastIndex, membershipVersion, leaderID)` trong cửa sổ `SyncDiscoveryWindow` (2 s).
2. **Target selection** — gom response theo `(commitIndex, hex(commitHash))`; chọn nhóm có nhiều phiếu nhất, tie-break bằng `commitIndex` cao nhất, làm sync target.
3. **Parallel fetch + verify** — chia range `[localCommit+1 .. target.CommitIndex]` thành các shard kích thước `SyncShardSize` (64 block). Mỗi shard mở libp2p stream qua `SyncProtocolID` tới một source khác (round-robin trên danh sách alive peer). Khi shard fail, thử source kế. Verify hash chain liên tục: mỗi `block.PrevHash` phải khớp hash trước, `block.BlockHash()` recompute phải khớp `block.Hash`, và hash của block cuối phải khớp `target.CommitHash`. Nếu verify fail → abort, không install. Sau khi block xong, fetch RaftLog entries `[target.CommitIndex+1 .. target.LogLastIndex]`.

Trong khi đang `Syncing`:
- Node bỏ qua `MsgBlockProposal` / `MsgBlockCommit` đến (sẽ catch-up qua sync target).
- Không tham gia leader election.
- Server-side sync handler từ chối phục vụ nếu chính nó cũng đang `Syncing` (tránh propagate dữ liệu chưa verify).

### Test sync — kịch bản gợi ý

**First-join**:
1. Cluster 2 node F0 + F1 đang chạy. Submit ~30 tx → ~10 block committed (leader tự động propose).
2. Khởi động F2, connect tới F0.
3. Gõ `status` tại F2: `OrderingBlock` phải khớp F0/F1. Log F2 có dòng `sync: fetching shard [..] from <peerID>`.

**Rejoin sau disconnect**:
1. Cluster 3 node F0 (leader), F1, F2 đang chạy.
2. Tại F0: `delay 30 1` (chặn heartbeat tới F1 trong 30 s, mô phỏng F1 isolated).
3. Submit tiếp tx trong 30 s → F0 + F2 có thêm block, F1 không có.
4. Sau 30 s, F0 resume heartbeat → F1 phát hiện gap → tự sync.

## Cấu hình Timeout

Các timeout mặc định trong `internal/network/protocol.go`:

| Parameter | Giá trị | Mô tả |
|-----------|---------|-------|
| `HeartbeatInterval` | 2 s | Khoảng thời gian giữa các heartbeat |
| `HeartbeatTimeout` | 5 s | Timeout để phát hiện leader failure |
| `DetectionTimeout` | 3 s | Timeout chờ expected leader gửi `MsgIAmNewLeader` |
| `SyncDiscoveryWindow` | 2 s | Cửa sổ thu `SyncStatusResponse` ở phase discovery |
| `SyncFetchTimeout` | 30 s | Deadline cho mỗi sync stream |
| `SyncShardSize` | 64 | Số block / log entry trên mỗi shard fetch song song |

## Protocol IDs

| Protocol ID | Mục đích |
|---|---|
| `/raft-order-service/1.0.0` | Stream chính cho `Message` (heartbeat, block, membership, sync status…) |
| `/raft-order-service/deliver/1.0.0` | Stream phát block tới committing peer |
| `/raft-order-service/endorsement/1.0.0` | Stream nhận endorsement transaction từ Core Service |
| `/raft-order-service/sync/1.0.0` | Stream catch-up block / RaftLog giữa các ordering node |

## Node states

| State | Mô tả |
|---|---|
| `Follower` | Bình thường: nhận block từ leader, gửi ack |
| `Leader` | Gửi heartbeat, gom tx, propose / commit block |
| `ClaimingLeader` | Đang gửi `MsgIAmNewLeader`, chờ majority `MsgLeaderClaimAck` |
| `Syncing` | Đang catch-up dữ liệu; tạm bỏ qua `BlockProposal/Commit` đến |

## Message types

```
MsgHeartbeat / MsgHeartbeatResponse
MsgIAmNewLeader / MsgLeaderClaimAck
MsgMembershipUpdate / MsgMembershipAck / MsgMembershipRequest / MsgMembershipResponse
MsgTxRequest / MsgTxResponse
MsgBlockProposal / MsgBlockProposalAck / MsgBlockCommit
MsgSyncStatusRequest / MsgSyncStatusResponse
```

## Lưu ý

- **Priority-based succession**: Node join trước có priority thấp hơn (ưu tiên cao hơn) và được chọn làm leader khi leader hiện tại fail
- **TxPool**: Transactions từ client được lưu với status `pending`; Leader tự động propose block mỗi 0.5 s với tối đa 20 tx/block
- **Block proposal**: Chỉ Leader mới có thể propose block; cần majority ACKs từ followers để commit; auto-propose luôn chạy khi node lên leader, không cần thao tác CLI
- **Deliver**: Committing peer kết nối qua protocol `/raft-order-service/deliver/1.0.0` để nhận blocks đã commit
- **Endorsement**: Core Service gửi endorsed transactions qua protocol `/raft-order-service/endorsement/1.0.0`
- **Data sync**: Node mới hoặc node rejoin sau disconnect tự động fetch song song blocks + RaftLog từ peers, verify hash chain trước khi install (xem mục "Đồng bộ dữ liệu" ở trên)

## Hạn chế hiện tại

- **Không có persistence**: `RaftLog` và `OrderingBlock` đều in-memory. Node restart = state rỗng (sync mechanism sẽ tự fetch lại từ cluster); nhưng nếu cả cluster restart cùng lúc thì state mất hoàn toàn.
- **Không có log compaction / snapshot**: `RaftLog.Entries` chỉ append, chưa có cơ chế trim cho cluster chạy lâu.
- **Sync không có signature**: hash-chain verification + majority quorum đủ chống một số dạng tampering ở mức demo, nhưng production cần ký dữ liệu.
- **Không có per-follower progress (nextIndex/matchIndex)**: catch-up hoàn toàn pull-based từ phía follower, leader không chủ động push cho follower lag.
- **Auth tầng app**: libp2p tự encrypt transport (Noise/TLS), nhưng chưa có authorization — bất kỳ peer nào cũng có thể gửi membership join.

## Troubleshooting

### Lỗi "no leader available"
- Đảm bảo có ít nhất một node đang chạy
- Chờ `HeartbeatTimeout` (5s) để cluster bầu leader mới
- Kiểm tra `status` để xem membership view và ai đang là leader

### Lỗi "failed to connect to peer"
- Kiểm tra địa chỉ node có đúng không (copy nguyên từ output của server)
- Đảm bảo node đích đang chạy
- Kiểm tra firewall không chặn port

### Transactions không được commit
- Đảm bảo tất cả nodes đã join cluster (kiểm tra bằng `status`)
- Leader tự động propose block mỗi 0.5 s; kiểm tra log leader có dòng `Auto-propose: proposing block` không
- Kiểm tra TxPool trên leader (hiển thị trong `status`)

### Leader không commit block
- Kiểm tra cluster còn đủ majority nodes không (cần ít nhất N/2 + 1 nodes alive)
- Kiểm tra kết nối giữa các nodes bằng `status` → Members list