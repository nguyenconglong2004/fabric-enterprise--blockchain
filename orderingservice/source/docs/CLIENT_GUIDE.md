# Hướng dẫn sử dụng Order Client

## Tổng quan

Order Client là một chương trình riêng biệt cho phép client gửi orders đến Raft Order Service cluster mà không cần chạy như một node trong cluster.

## Cài đặt và chạy

### Build client

Client cần các types từ package main, nên cần build cùng với các file khác (trừ main.go):

**Linux/Mac:**
```bash
chmod +x build_client.sh
./build_client.sh
```

**Windows:**
```cmd
build_client.bat
```

**Hoặc build thủ công:**
```bash
go build -o client client.go node.go types.go consensus.go order_service.go
```

**Hoặc chạy trực tiếp:**
```bash
go run -tags client client.go node.go types.go consensus.go order_service.go
```
/ip4/127.0.0.1/tcp/6000/p2p/12D3KooWGoXDzbzupAn2grM5CSpE3Scg2q9wVSnVC4WP4FETiuf9
**Lưu ý:** Client sử dụng build tag `client` để tránh conflict với main.go khi build.

## Sử dụng

### Bước 1: Khởi động Order Service Cluster

Trước tiên, bạn cần có ít nhất một node trong cluster đang chạy:

```bash
# Terminal 1 - Start node 1 (leader)
go run .
# Port: 6000
# First node: y
```

### Bước 2: Khởi động Client

```bash
# Terminal 2 - Start client
go run client.go
# hoặc
./client
```

### Bước 3: Kết nối đến cluster

Client sẽ yêu cầu địa chỉ của một node trong cluster:

```
Enter address of a node in the cluster (e.g., /ip4/127.0.0.1/tcp/6000/p2p/...):
```

Nhập địa chỉ của node (copy từ output của node khi start, ví dụ: `/ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...`)

### Bước 4: Submit orders

Sau khi kết nối thành công, bạn có thể submit orders:

```
> order Buy 100 BTC
> order Sell 50 ETH
> order Buy 200 ADA
```

Client sẽ:
1. Gửi order request đến node đã kết nối
2. Node sẽ forward đến leader (nếu không phải leader)
3. Leader xử lý và commit order
4. Leader gửi response về client
5. Client hiển thị kết quả

## Flow hoạt động

### Scenario 1: Client kết nối đến Leader

1. Client gửi `MsgOrderRequest` đến leader
2. Leader nhận → Commit order → Replicate đến followers
3. Leader gửi `MsgOrderResponse` về client
4. Client hiển thị kết quả

### Scenario 2: Client kết nối đến Follower

1. Client gửi `MsgOrderRequest` đến follower
2. Follower nhận → Kiểm tra sender không phải member → Forward đến leader
3. Leader nhận → Commit order → Replicate đến followers
4. Leader gửi `MsgOrderResponse` về client (giữ nguyên SenderID)
5. Client nhận response và hiển thị kết quả

## Commands

- `order <data>` - Submit một order mới
  - Ví dụ: `order Buy 100 BTC`
  - Ví dụ: `order Sell 50 ETH`

- `quit` hoặc `exit` - Thoát client

- `help` - Hiển thị danh sách commands

## Ví dụ sử dụng

### Terminal 1: Node 1 (Leader)
```bash
$ go run .
Enter port for this node (e.g., 6000): 6000
Is this the first node? (y/n): y

Node started successfully!
Node ID: <peer.ID 12*XXXXXX>
Address: /ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...
```

### Terminal 2: Client
```bash
$ go run client.go
=== Raft Order Service Client ===

Enter address of a node in the cluster: /ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...
Connected to node: <peer.ID 12*XXXXXX>

=== Commands ===
1. order <data> - Submit an order (e.g., order Buy 100 BTC)
2. quit - Exit

> order Buy 100 BTC
Order request sent: order-1702845123456789000 (waiting for response...)

✓ Order submitted successfully!
  Order ID: order-1702845123456789000
  Data: Buy 100 BTC
  Status: committed
  Timestamp: 2025-12-17 23:25:10
```

## Lưu ý

1. **Client không phải là node**: Client không tham gia vào consensus protocol, chỉ gửi orders
2. **Kết nối đến bất kỳ node nào**: Client có thể kết nối đến leader hoặc follower, follower sẽ tự động forward đến leader
3. **Response từ leader**: Leader sẽ gửi response trực tiếp về client (không qua follower)
4. **Order ID**: Order ID được tạo bởi client dựa trên timestamp, đảm bảo unique

## Troubleshooting

### Lỗi: "failed to connect to node"
- Kiểm tra node đã start chưa
- Kiểm tra địa chỉ node có đúng không
- Kiểm tra firewall/network

### Lỗi: "No leader available"
- Đảm bảo cluster có ít nhất một node đang chạy
- Đợi một chút để leader được elect (nếu vừa start)

### Không nhận được response
- Kiểm tra logs của node để xem order có được xử lý không
- Đảm bảo leader đang hoạt động
- Thử submit lại order

