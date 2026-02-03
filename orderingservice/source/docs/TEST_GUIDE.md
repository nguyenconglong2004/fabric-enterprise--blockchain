# Hướng dẫn Test Raft Order Service

## Các sửa đổi đã thực hiện

### 1. Sửa lỗi Membership Serialization
- **Vấn đề**: `join_time` không được serialize đúng format khi gửi qua mạng
- **Giải pháp**: Chuyển đổi `time.Time` thành string format RFC3339Nano trước khi serialize
- **File**: [consensus.go](consensus.go:167)

### 2. Node Step Down khi phát hiện Leader có priority cao hơn
- **Vấn đề**: Node thứ 2 tự động lên leader khi khởi động, không chịu step down khi kết nối với leader có priority cao hơn
- **Giải pháp**: Thêm logic kiểm tra trong `handleMembershipAck` để node tự động step down nếu phát hiện leader khác có priority cao hơn
- **File**: [consensus.go](consensus.go:122-139)

### 3. Cải thiện Leader Failure Detection
- **Vấn đề**: Khi leader chết, follower không tự động lên làm leader mới
- **Giải pháp**:
  - Đánh dấu old leader là dead TRƯỚC KHI đếm alive members
  - Thêm logging chi tiết để debug quá trình claim leadership
- **File**: [consensus.go](consensus.go:401-440)

### 4. Phân biệt Membership Update types
- **Vấn đề**: Không phân biệt giữa "join request" và "broadcast membership update"
- **Giải pháp**: Kiểm tra cấu trúc data để phân biệt hai loại message
- **File**: [consensus.go](consensus.go:58-66)

## Test Case 1: Khởi tạo Cluster với 2 Nodes

### Bước 1: Start Node 1 (Bootstrap)
```bash
go run .
```
Input:
- Port: `6000`
- First node: `y`

**Kết quả mong đợi:**
```
[<peer.ID 12*XXXXXX>] Node created with ID: 12D3Koo...
[<peer.ID 12*XXXXXX>] Starting node
[<peer.ID 12*XXXXXX>] *** I AM NOW THE LEADER (term 0) ***
```

### Bước 2: Check Status Node 1
```
> status
```

**Kết quả mong đợi:**
```
=== Node Status ===
State: Leader
=== Membership ===
Total alive members: 1
  - Priority 0: <peer.ID> (LEADER) (SELF)
```

### Bước 3: Start Node 2
Mở terminal mới:
```bash
go run .
```
Input:
- Port: `6001`
- First node: `n`
- Connect to: `<copy địa chỉ từ Node 1>`

**Kết quả mong đợi:**
```
[<peer.ID 12*YYYYYY>] Connected to peer: <peer.ID 12*XXXXXX>
[<peer.ID 12*YYYYYY>] Received membership ack from ...
[<peer.ID 12*YYYYYY>] Stepping down, discovered higher priority leader: ...
[<peer.ID 12*YYYYYY>] Updated membership view, current leader: ...
```

### Bước 4: Check Status cả 2 nodes
Node 1:
```
> status
```
Expected: State: Leader, 2 members

Node 2:
```
> status
```
Expected: State: Follower, Leader: Node 1, 2 members

## Test Case 2: Leader Failover

### Setup: Cluster với 2 nodes đang chạy

### Bước 1: Submit một vài orders từ Node 1 (leader)
```
> order Buy 100 BTC
> order Sell 50 ETH
> orders
```

### Bước 2: Kill Node 1 (leader)
Tại terminal của Node 1, nhấn `Ctrl+C`

### Bước 3: Quan sát Node 2
Sau ~5 giây (HeartbeatTimeout), bạn sẽ thấy:

**Logs mong đợi tại Node 2:**
```
[<peer.ID>] Heartbeat timeout! Last heartbeat: 5.xxx ago
[<peer.ID>] Proposing leader <old_leader> is down
[<peer.ID>] Leader down votes: 1/1 alive nodes (need majority: 1)
[<peer.ID>] Consensus reached: leader is down
[<peer.ID>] Marked old leader <old_leader> as dead
[<peer.ID>] Current alive members:
  - <peer.ID> (priority: 1)
[<peer.ID>] I have highest priority (1), claiming leadership
[<peer.ID>] *** I AM NOW THE LEADER (term 1) ***
```

### Bước 4: Check Status Node 2
```
> status
```

**Kết quả mong đợi:**
```
=== Node Status ===
State: Leader
Term: 1
=== Membership ===
Total alive members: 1
  - Priority 1: <peer.ID> (LEADER) (SELF)
```

### Bước 5: Submit order mới từ Node 2
```
> order Test order after failover
> orders
```

**Kết quả mong đợi:**
- Order mới được commit thành công
- Tất cả orders cũ vẫn còn (nếu đã được replicate)

## Test Case 3: Cluster với 3 Nodes và Priority-based Succession

### Setup: Start 3 nodes theo thứ tự

**Node 1** (Priority 0 - highest):
```bash
go run .
# Port: 6000, First: y
```

**Node 2** (Priority 1):
```bash
go run .
# Port: 6001, First: n, Connect: <Node 1 address>
```

**Node 3** (Priority 2):
```bash
go run .
# Port: 6002, First: n, Connect: <Node 1 address>
```

### Kiểm tra Status tại Node 1:
```
> status
```

**Kết quả mong đợi:**
```
=== Membership ===
Total alive members: 3
  - Priority 0: <Node1> (LEADER) (SELF)
  - Priority 1: <Node2>
  - Priority 2: <Node3>
```

### Test Failover Chain:

#### Scenario A: Kill Priority 0 (Node 1)
1. Kill Node 1
2. Observe: Node 2 (Priority 1) sẽ lên làm leader
3. Check status Node 2: State should be Leader

#### Scenario B: Kill Priority 1 (Node 2 - current leader)
1. Kill Node 2
2. Observe: Node 3 (Priority 2) sẽ lên làm leader
3. Check status Node 3: State should be Leader

## Test Case 4: Order Replication

### Setup: 3 nodes cluster

### Bước 1: Submit orders từ leader
```
> order Order 1
> order Order 2
> order Order 3
```

### Bước 2: Check orders tại tất cả nodes
Node 1: `> orders`
Node 2: `> orders`
Node 3: `> orders`

**Kết quả mong đợi:** Tất cả nodes có cùng danh sách orders

### Bước 3: Submit order từ follower
Tại Node 2 (follower):
```
> order Order from follower
```

**Kết quả mong đợi:**
- Order được forward đến leader
- Leader commit và replicate về tất cả nodes
- Check `orders` tại tất cả nodes → tất cả có order mới

## Debugging Tips

### 1. Nếu Node không lên làm leader sau khi leader cũ chết:

Check logs:
- Có thấy "Heartbeat timeout"? → Nếu không, tăng thời gian chờ
- Có thấy "Proposing leader X is down"? → Heartbeat detection đang hoạt động
- Có thấy "Consensus reached"? → Đủ votes
- Có thấy "Current alive members"? → Check xem có đúng membership không
- Có thấy "I have highest priority"? → Node đang claim leadership

### 2. Nếu có 2 leaders cùng lúc (split brain):

Check:
- Membership view có giống nhau không? `status` tại mỗi node
- Term có tăng đúng không?
- Node có step down khi nhận heartbeat từ leader term cao hơn không?

### 3. Nếu node mới join bị reject:

Check:
- Node bootstrap có phải là leader không?
- Network connectivity: `connect <address>` có thành công không?
- Logs tại leader: có thấy "Received membership update" không?

## Expected Timing

- **Heartbeat Interval**: 2 giây
- **Heartbeat Timeout**: 5 giây (thời gian để phát hiện leader chết)
- **Detection Timeout**: 3 giây (thời gian chờ consensus về leader failure)
- **Total Failover Time**: ~8-10 giây từ khi leader chết đến khi leader mới được elect

## Common Issues và Solutions

### Issue 1: "Error decoding peer ID: invalid cid"
- **Nguyên nhân**: Đã fix - join_time serialization
- **Solution**: Rebuild project với code mới

### Issue 2: Node tự động lên leader khi join
- **Nguyên nhân**: Đã fix - step down logic
- **Solution**: Rebuild project với code mới

### Issue 3: Node không failover khi leader chết
- **Nguyên nhân**: Đã fix - mark leader dead before counting alive members
- **Solution**: Rebuild project với code mới
