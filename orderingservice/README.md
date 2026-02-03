# Raft-based Order Service with Priority-based Leader Succession

Một order service được xây dựng trên cơ chế Raft consensus sử dụng go-libp2p, với cơ chế leader succession dựa trên độ ưu tiên thay vì bầu cử truyền thống.

## Đặc điểm chính

### 1. Đồng thuận về "Bảng danh sách ưu tiên" (Membership View)
- Mọi node có cùng một danh sách thành viên với thông tin thời gian tham gia
- Node tham gia càng sớm thì độ ưu tiên càng cao (priority number càng thấp)
- Membership updates phải được đồng thuận bởi leader và replicate đến tất cả followers

### 2. Cơ chế Phát hiện và Xác nhận Leader chết (Detection Phase)
- Node phát hiện leader chết khi không nhận được heartbeat trong thời gian timeout
- Node gửi `Leader_Down_Proposal` kèm chữ ký
- Chỉ chuyển sang tìm leader mới khi nhận được đủ N/2 + 1 xác nhận (majority)

### 3. Quy trình "Tuyên bố quyền lực" (Claim Phase)
- Node có độ ưu tiên cao nhất trong các node còn sống gửi `I_AM_LEADER`
- Kèm theo bằng chứng về độ ưu tiên và Term mới
- Followers kiểm tra tính hợp lệ và gửi ACK
- Leader được công nhận khi nhận đủ majority ACKs

## Cấu trúc dự án

```
.
├── main.go           # Entry point và interactive CLI
├── node.go           # Core node implementation
├── types.go          # Data structures và types
├── consensus.go      # Consensus protocol implementation
├── order_service.go  # Order processing logic
├── client.go         # Order client (gửi orders từ bên ngoài)
└── CLIENT_GUIDE.md   # Hướng dẫn sử dụng client
```

## Cài đặt

### Yêu cầu
- Go 1.21 hoặc cao hơn

### Cài đặt dependencies

```bash
go mod tidy
```

## Sử dụng

### Chạy node đầu tiên (Bootstrap node)

```bash
go run .
```

Khi được hỏi:
- Nhập port (ví dụ: 6000)
- Chọn "y" khi được hỏi "Is this the first node?"

Node đầu tiên sẽ tự động trở thành leader.

### Chạy các node tiếp theo

Mở terminal mới và chạy:

```bash
go run .
```

Khi được hỏi:
- Nhập port khác (ví dụ: 6001, 6002, ...)
- Chọn "n" khi được hỏi "Is this the first node?"
- Nhập địa chỉ của node đầu tiên (copy từ output của node đầu tiên)

### Commands

Trong interactive mode, bạn có thể sử dụng các lệnh sau:

- `status` - Hiển thị trạng thái hiện tại của node (state, term, leader, membership, orders)
- `order <data>` - Submit một order mới (ví dụ: `order Buy 100 BTC`)
- `orders` - Liệt kê tất cả orders đã commit
- `connect <address>` - Kết nối đến một node khác
- `quit` - Thoát chương trình

### Sử dụng Order Client (Khuyến nghị)

Thay vì submit orders từ các nodes, bạn có thể sử dụng Order Client riêng biệt:

```bash
# Terminal riêng - Start client
go run client.go
```

Client sẽ kết nối đến một node trong cluster và gửi orders. Xem chi tiết trong [CLIENT_GUIDE.md](CLIENT_GUIDE.md).

## Demo kịch bản

### Kịch bản 1: Khởi tạo cluster và submit orders

1. **Terminal 1** - Start node đầu tiên:
```bash
go run .
# Port: 6000
# First node: y
```

2. **Terminal 2** - Start node thứ hai:
```bash
go run .
# Port: 6001
# First node: n
# Connect to: /ip4/127.0.0.1/tcp/6000/p2p/<NODE1_ID>
```

3. **Terminal 3** - Start node thứ ba:
```bash
go run .
# Port: 6002
# First node: n
# Connect to: /ip4/127.0.0.1/tcp/6000/p2p/<NODE1_ID>
```

4. Kiểm tra status ở mọi node:
```
> status
```

5. Submit orders từ bất kỳ node nào:
```
> order Buy 100 BTC
> order Sell 50 ETH
> order Buy 200 ADA
```

6. Kiểm tra orders đã được replicate:
```
> orders
```

### Kịch bản 2: Test Leader Failure Recovery

1. Với cluster 3 nodes đang chạy, kiểm tra ai là leader:
```
> status
```

2. Kill leader node (Ctrl+C tại terminal của leader)

3. Quan sát các follower nodes:
   - Sau 5 giây (heartbeat timeout), các node sẽ phát hiện leader chết
   - Gửi `Leader_Down_Proposal`
   - Đợi consensus (majority votes)
   - Node có priority cao nhất sẽ claim leadership
   - Các follower acknowledge leader mới

4. Kiểm tra status tại các node còn lại:
```
> status
```

5. Submit order mới để verify leader mới hoạt động:
```
> order Test order after leader change
```

### Kịch bản 3: Membership Priority

1. Start nodes theo thứ tự với delays:
```bash
# Terminal 1 (Priority 0 - highest)
go run .  # Port 6000, first node

# Đợi 2 giây

# Terminal 2 (Priority 1)
go run .  # Port 6001, connect to node 1

# Đợi 2 giây

# Terminal 3 (Priority 2)
go run .  # Port 6002, connect to node 1
```

2. Kiểm tra membership view:
```
> status
```

3. Kill node priority 0 (leader), observe priority 1 becomes leader
4. Kill node priority 1, observe priority 2 becomes leader

## Chi tiết kỹ thuật

### Message Types

- `MsgHeartbeat` - Leader gửi định kỳ để báo hiệu còn sống
- `MsgLeaderDownProposal` - Đề xuất leader đã chết
- `MsgLeaderDownAck` - Xác nhận đồng ý leader đã chết
- `MsgIAmLeader` - Tuyên bố làm leader mới
- `MsgLeaderAck` - Xác nhận chấp nhận leader mới
- `MsgMembershipUpdate` - Cập nhật membership view
- `MsgMembershipAck` - Xác nhận membership update
- `MsgOrderRequest` - Request xử lý order
- `MsgOrderResponse` - Response cho order request

### Timeouts

- `HeartbeatInterval`: 2 giây - Khoảng thời gian giữa các heartbeat
- `HeartbeatTimeout`: 5 giây - Thời gian chờ heartbeat trước khi nghi ngờ leader chết
- `DetectionTimeout`: 3 giây - Thời gian chờ consensus về leader failure

### Node States

- `Follower` - Trạng thái bình thường, nhận lệnh từ leader
- `Leader` - Node đang là leader, gửi heartbeat và xử lý orders
- `DetectingLeaderFailure` - Đang trong quá trình phát hiện và đồng thuận về leader failure

## Lưu ý

- Đây là implementation đơn giản cho mục đích học tập và demo
- Trong production, cần thêm:
  - Persistent storage cho log
  - Proper log replication với acknowledgments
  - Snapshot và log compaction
  - Security (authentication, encryption)
  - Network partition handling
  - More robust error handling

## Tài liệu tham khảo

- [Raft Consensus Algorithm](https://raft.github.io/)
- [go-libp2p Documentation](https://docs.libp2p.io/)
