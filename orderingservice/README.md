# Raft Order Service - Hướng dẫn sử dụng

## Giới thiệu

Implementation của Raft consensus protocol với priority-based leader succession. Service nhận transactions từ client (hoặc Core Service), đóng gói thành blocks, và phân phối đến committing peers.

## Cấu trúc Project

```
source/
├── cmd/
│   ├── server/main.go        # Server node (interactive CLI)
│   └── client/main.go        # External UTXO client
├── internal/
│   ├── raft/                 # Core Raft logic
│   ├── network/              # Network layer (libp2p)
│   └── types/                # Data structures
├── pkg/
│   └── client/               # Public client API
├── docs/                     # Documentation
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
| `propose [n]` | Propose block với tối đa n transactions từ pool (chỉ Leader; mặc định n=3) |
| `autoblock start` | Bật tự động propose block khi pool đủ tx (chỉ Leader) |
| `autoblock stop` | Tắt auto-propose |
| `delay <secs> <p1> [p2]...` | Delay heartbeat đến các node có priority chỉ định X giây (chỉ Leader; dùng để test) |
| `connect <addr>` | Kết nối đến node khác |
| `help` | Hiển thị danh sách commands |
| `quit` | Thoát |

### Ví dụ workflow trên Leader:

```bash
> status                    # Kiểm tra trạng thái
> propose 5                 # Propose block với tối đa 5 tx từ pool
> autoblock start           # Bật auto-propose (tự động propose khi pool đủ tx)
> autoblock stop            # Tắt auto-propose
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

## Cấu hình Timeout

Các timeout mặc định trong `internal/network/protocol.go`:

| Parameter | Giá trị | Mô tả |
|-----------|---------|-------|
| `HeartbeatInterval` | 2s | Khoảng thời gian giữa các heartbeat |
| `HeartbeatTimeout` | 5s | Timeout để phát hiện leader failure |
| `DetectionTimeout` | 3s | Timeout chờ expected leader gửi `MsgIAmNewLeader` |

## Lưu ý

- **Priority-based succession**: Node join trước có priority thấp hơn (ưu tiên cao hơn) và được chọn làm leader khi leader hiện tại fail
- **TxPool**: Transactions từ client được lưu với status `pending`; cần Leader `propose` block để đưa vào Raft log rồi commit
- **Block proposal**: Chỉ Leader mới có thể propose block; cần majority ACKs từ followers để commit
- **Auto-propose**: Leader có thể bật `autoblock start` để tự động propose block theo chu kỳ
- **Deliver**: Committing peer kết nối qua protocol `/raft-order-service/deliver/1.0.0` để nhận blocks đã commit
- **Endorsement**: Core Service gửi endorsed transactions qua protocol `/raft-order-service/endorsement/1.0.0`

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
- Cần Leader chạy `propose [n]` hoặc bật `autoblock start` để đưa transactions vào block
- Kiểm tra TxPool trên leader (hiển thị trong `status`)

### Leader không commit block
- Kiểm tra cluster còn đủ majority nodes không (cần ít nhất N/2 + 1 nodes alive)
- Kiểm tra kết nối giữa các nodes bằng `status` → Members list