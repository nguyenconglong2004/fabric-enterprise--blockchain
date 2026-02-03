# Raft Order Service - Hướng dẫn sử dụng

## Giới thiệu

Đây là implementation của Raft consensus protocol sử dụng priority-based leader succession thay vì election-based. Service cho phép submit và quản lý orders trong một cluster phân tán.

## Cấu trúc Project

```
new/
├── cmd/
│   ├── server/main.go        # Server node
│   └── client/main.go        # External client
├── internal/
│   ├── raft/                 # Core Raft logic
│   ├── network/              # Network layer (libp2p)
│   └── types/                # Data structures
├── pkg/
│   └── client/               # Public client API
└── docs/                     # Documentation
```

## Build

```bash
cd new

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
Enter port for this node (e.g., 6000): 6000
Is this the first node? (y/n): y
```

Node đầu tiên sẽ tự động trở thành **Leader**. Ghi lại địa chỉ hiển thị, ví dụ:
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
Enter port for this node (e.g., 6000): 6001
Is this the first node? (y/n): n
Enter address of existing node to connect to: /ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...
```

### Bước 3: Khởi động thêm Node (tùy chọn)

Lặp lại bước 2 với port khác (6002, 6003, ...).

## Sử dụng Server Commands

Sau khi server khởi động, bạn có thể sử dụng các lệnh sau:

| Command | Mô tả |
|---------|-------|
| `status` | Hiển thị trạng thái node (ID, State, Term, Leader, Members, Orders) |
| `order <data>` | Submit một order mới (ví dụ: `order Buy 100 BTC`) |
| `orders` | Liệt kê tất cả orders |
| `propose <idx1> [idx2] ...` | Propose block với các orders (chỉ Leader, ví dụ: `propose 1 2 3`) |
| `connect <address>` | Kết nối đến node khác |
| `help` | Hiển thị danh sách commands |
| `quit` | Thoát |

### Ví dụ workflow trên Leader:

```bash
> status                    # Kiểm tra trạng thái
> order Buy 100 BTC         # Submit order 1
> order Sell 50 ETH         # Submit order 2
> order Buy 200 USDT        # Submit order 3
> orders                    # Xem danh sách orders
> propose 1 2 3             # Propose block với 3 orders
> status                    # Kiểm tra orders đã committed
```

## Sử dụng Client

Client cho phép submit orders từ bên ngoài cluster.

### Khởi động Client

```bash
./client
```

Nhập địa chỉ của một node trong cluster:
```
Enter address of a node in the cluster: /ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...
```

### Client Commands

| Command | Mô tả |
|---------|-------|
| `order <data>` | Submit order đến tất cả nodes (ví dụ: `order Buy 500 BTC`) |
| `help` | Hiển thị danh sách commands |
| `quit` | Thoát |

### Ví dụ:

```bash
> order Buy 500 BTC
Order request sent to all nodes: order-1706012345678 (waiting for response...)
```

## Test Leader Failover

1. Khởi động 3 nodes (port 6000, 6001, 6002)
2. Node 6000 là Leader (priority 0)
3. Tắt node 6000 (Ctrl+C)
4. Sau 5 giây, node 6001 (priority 1) sẽ tự động trở thành Leader mới
5. Kiểm tra bằng lệnh `status` trên các node còn lại

## Cấu hình Timeout

Các timeout mặc định (trong `internal/network/protocol.go`):

| Parameter | Giá trị | Mô tả |
|-----------|---------|-------|
| HeartbeatInterval | 2s | Khoảng thời gian giữa các heartbeat |
| HeartbeatTimeout | 5s | Timeout để phát hiện leader failure |
| DetectionTimeout | 3s | Timeout chờ consensus về leader death |

## Lưu ý

- **Priority-based succession**: Node join trước có priority cao hơn và sẽ được chọn làm leader khi leader hiện tại fail
- **Orders**: Orders từ client được lưu với status `pending`, cần Leader `propose` block để commit
- **Block proposal**: Chỉ Leader mới có thể propose block, cần majority ACKs để commit

## Troubleshooting

### Lỗi "no leader available"
- Đảm bảo có ít nhất một node đang chạy là Leader
- Chờ heartbeat timeout (5s) để cluster elect leader mới

### Lỗi "failed to connect to peer"
- Kiểm tra địa chỉ node có đúng không
- Đảm bảo node đích đang chạy
- Kiểm tra firewall không chặn port

### Orders không được replicate
- Đảm bảo tất cả nodes đã join cluster (kiểm tra bằng `status`)
- Orders từ client cần Leader propose block để commit
