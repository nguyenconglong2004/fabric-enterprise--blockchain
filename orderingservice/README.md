# Raft-based Ordering Service with Priority-based Leader Succession

Một ordering service được xây dựng trên cơ chế Raft consensus sử dụng go-libp2p, với cơ chế leader succession dựa trên độ ưu tiên (join time) thay vì bầu cử truyền thống. Service nhận giao dịch từ client, đóng gói thành blocks, và phân phối đến các committing peer.

## Đặc điểm chính

### 1. Đồng thuận về "Bảng danh sách ưu tiên" (Membership View)
- Mọi node có cùng một danh sách thành viên với thông tin thời gian tham gia
- Node tham gia càng sớm thì độ ưu tiên càng cao (priority number càng thấp)
- Membership updates được leader quản lý và replicate đến tất cả followers

### 2. Cơ chế Phát hiện Leader chết (Heartbeat Timeout)
- Node phát hiện leader chết khi không nhận được heartbeat trong `HeartbeatTimeout` (5 giây)
- Node đánh dấu leader cũ là dead trong membership view
- Node có priority cao nhất trong số các node còn sống tự chuyển sang Claim Phase
- Các node khác chờ nhận `MsgIAmNewLeader` từ expected leader; nếu quá deadline thì đánh dấu chết và thử node kế tiếp

### 3. Quy trình "Tuyên bố quyền lực" (Claim Phase)
- Node có priority cao nhất gửi `MsgIAmNewLeader` kèm term mới và priority
- Followers kiểm tra tính hợp lệ và gửi `MsgLeaderClaimAck`
- Leader được công nhận khi nhận đủ majority ACKs (N/2 + 1)

### 4. Block-based Ordering
- Transactions từ client được lưu vào TxPool (pending pool)
- Leader propose block chứa N transactions từ pool
- Followers gửi `MsgBlockProposalAck` sau khi ghi vào Raft log
- Khi đủ majority ACKs, leader broadcast `MsgBlockCommit` để commit block
- Block đã commit được fan-out đến các committing peer qua Deliver service

### 5. Deliver Service
- Committing peer kết nối qua protocol `/raft-order-service/deliver/1.0.0`
- Nhận toàn bộ lịch sử block từ `fromIndex` chỉ định
- Tự động nhận các block mới khi được commit

### 6. Endorsement Service
- Nhận endorsed transactions từ Core Service qua protocol `/raft-order-service/endorsement/1.0.0`
- Nếu node nhận không phải leader, tự động forward đến leader
- Leader thêm transaction vào TxPool

## Cấu trúc dự án

```
orderingservice/
└── source/
    ├── cmd/
    │   ├── server/main.go        # Server node (interactive CLI)
    │   └── client/main.go        # External UTXO client
    ├── internal/
    │   ├── raft/                 # Core Raft logic
    │   │   ├── node.go           # RaftNode struct và lifecycle
    │   │   ├── consensus.go      # Message processing & consensus
    │   │   ├── leader.go         # Leader election & claim phase
    │   │   ├── heartbeat.go      # Heartbeat sending & monitoring
    │   │   ├── membership.go     # Membership management
    │   │   ├── transaction.go    # TxPool & block proposal
    │   │   ├── deliver.go        # Deliver service (block fan-out)
    │   │   └── endorsement.go    # Endorsement stream handler
    │   ├── network/
    │   │   ├── transport.go      # libp2p transport layer
    │   │   └── protocol.go       # Protocol IDs & timeout constants
    │   └── types/                # Data structures
    │       ├── block.go          # Block types
    │       ├── transaction.go    # Transaction (UTXO + smart contract)
    │       ├── message.go        # Message types
    │       ├── membership.go     # MembershipView
    │       ├── state.go          # NodeState
    │       ├── deliver.go        # DeliverRequest
    │       └── sig.go            # Ed25519 helpers
    ├── pkg/
    │   └── client/client.go      # Public client API
    ├── docs/                     # Documentation
    └── testingscenarios/         # Test scenario descriptions
```

## Cài đặt

### Yêu cầu
- Go 1.21 hoặc cao hơn

### Build

```bash
cd source

# Build server
go build -o server ./cmd/server

# Build client
go build -o client ./cmd/client

# Hoặc build cả hai
go build ./...
```

## Sử dụng

### Chạy node đầu tiên (Bootstrap node)

```bash
./server
# Enter port for P2P network (e.g., 6000): 6000
# Is this the first node? (y/n): y
```

Node đầu tiên tự động trở thành leader. Ghi lại địa chỉ hiển thị:
```
Address: /ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...
```

### Chạy các node tiếp theo

```bash
./server
# Enter port for P2P network (e.g., 6000): 6001
# Is this the first node? (y/n): n
# Enter address of existing node to connect to: /ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...
```

### Server Commands

| Command | Mô tả |
|---------|-------|
| `status` | Hiển thị trạng thái node (ID, State, Term, Leader, Membership, TxPool, Raft log, Ordering blocks) |
| `propose [n]` | Propose block với tối đa n transactions từ pool (chỉ Leader; mặc định n=3) |
| `autoblock start` | Bật tự động propose block khi pool đủ tx (chỉ Leader) |
| `autoblock stop` | Tắt auto-propose |
| `delay <secs> <p1> [p2]...` | Delay heartbeat đến các node có priority chỉ định (chỉ Leader; dùng để test) |
| `connect <addr>` | Kết nối đến node khác |
| `help` | Hiển thị danh sách commands |
| `quit` | Thoát |

## Demo kịch bản

### Kịch bản 1: Khởi tạo cluster và xử lý transactions

1. **Terminal 1** - Start node đầu tiên (Leader):
```bash
./server
# Port: 6000, First node: y
```

2. **Terminal 2** - Start node thứ hai:
```bash
./server
# Port: 6001, First node: n, Connect to: /ip4/127.0.0.1/tcp/6000/p2p/<NODE1_ID>
```

3. **Terminal 3** - Start node thứ ba:
```bash
./server
# Port: 6002, First node: n, Connect to: /ip4/127.0.0.1/tcp/6000/p2p/<NODE1_ID>
```

4. **Terminal 4** - Start client và gửi transactions:
```bash
./client
# Enter address: /ip4/127.0.0.1/tcp/6000/p2p/<NODE1_ID>
> keygen
> fund 100000
> tx <to_addr> 100
```

5. Trên Leader, propose block:
```
> propose 3
```

### Kịch bản 2: Test Leader Failure Recovery

1. Với cluster 3 nodes đang chạy, xác nhận leader bằng `status`

2. Kill leader node (Ctrl+C)

3. Quan sát các follower:
   - Sau 5 giây (`HeartbeatTimeout`), node priority cao nhất phát hiện timeout
   - Gửi `MsgIAmNewLeader` kèm term mới
   - Followers gửi `MsgLeaderClaimAck`
   - Leader mới được công nhận sau khi nhận đủ majority ACKs

4. Kiểm tra `status` tại các node còn lại

### Kịch bản 3: Test Network Isolation

```bash
# Trên Leader (priority 0):
> delay 15 1 2    # Delay heartbeat 15 giây đến node priority 1 và 2
```

Nodes priority 1 và 2 sẽ timeout và bầu lại leader sau `HeartbeatTimeout`.

### Kịch bản 4: Membership Priority

```bash
# Terminal 1 (Priority 0 - highest, Leader đầu tiên)
./server  # Port 6000, first node

# Đợi 2 giây

# Terminal 2 (Priority 1)
./server  # Port 6001, connect to node 1

# Đợi 2 giây

# Terminal 3 (Priority 2)
./server  # Port 6002, connect to node 1
```

Kill node priority 0 → node priority 1 trở thành leader.
Kill node priority 1 → node priority 2 trở thành leader.

## Chi tiết kỹ thuật

### Message Types

| Message | Mô tả |
|---------|-------|
| `MsgHeartbeat` | Leader gửi định kỳ để báo hiệu còn sống |
| `MsgHeartbeatResponse` | Follower phản hồi heartbeat từ leader lỗi thời (kèm term và leader hiện tại) |
| `MsgIAmNewLeader` | Node priority cao nhất tuyên bố làm leader mới |
| `MsgLeaderClaimAck` | Follower xác nhận (YES/NO) leader mới |
| `MsgMembershipUpdate` | Yêu cầu tham gia hoặc cập nhật membership view |
| `MsgMembershipAck` | Xác nhận membership update |
| `MsgMembershipRequest` | Yêu cầu lấy membership view hiện tại |
| `MsgMembershipResponse` | Phản hồi membership view |
| `MsgTxRequest` | Client gửi transaction đến node |
| `MsgTxResponse` | Node phản hồi kết quả nhận transaction |
| `MsgBlockProposal` | Leader đề xuất block mới (kèm danh sách transactions) |
| `MsgBlockProposalAck` | Follower xác nhận đã ghi block vào Raft log |
| `MsgBlockCommit` | Leader broadcast lệnh commit block sau khi đủ majority ACKs |

### Protocols (libp2p)

| Protocol ID | Mục đích |
|-------------|----------|
| `/raft-order-service/1.0.0` | Giao tiếp Raft giữa các nodes |
| `/raft-order-service/deliver/1.0.0` | Deliver blocks đến committing peer |
| `/raft-order-service/endorsement/1.0.0` | Nhận endorsed transactions từ Core Service |

### Timeouts

| Parameter | Giá trị | File cấu hình |
|-----------|---------|---------------|
| `HeartbeatInterval` | 2s | `internal/network/protocol.go` |
| `HeartbeatTimeout` | 5s | `internal/network/protocol.go` |
| `DetectionTimeout` | 3s | `internal/network/protocol.go` |

### Node States

| State | Mô tả |
|-------|-------|
| `Follower` | Trạng thái bình thường; nhận heartbeat và blocks từ leader |
| `Leader` | Gửi heartbeat, quản lý TxPool, propose và commit blocks |
| `ClaimingLeader` | Đang gửi `MsgIAmNewLeader` và chờ majority ACKs để lên leader |

### Transaction Types

Service hỗ trợ hai loại transaction:

- **UTXO Transaction**: Bitcoin-style P2PKH, ký bằng Ed25519, dùng cho client UTXO wallet
- **Smart Contract Transaction**: Chứa `payload`, `contract_name`, `function_name`, `endorsements` từ Core Service

## Tài liệu tham khảo

- [Raft Consensus Algorithm](https://raft.github.io/)
- [go-libp2p Documentation](https://docs.libp2p.io/)
