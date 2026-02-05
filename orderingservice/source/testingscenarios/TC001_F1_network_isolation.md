# TC001: F1 Bị Mất Kết Nối Với Leader và Các Followers

## Thông Tin Kịch Bản

| Thuộc tính | Giá trị |
|------------|---------|
| **Mã kịch bản** | TC001 |
| **Loại lỗi** | Network Isolation (Node bị cô lập) |
| **Node bị ảnh hưởng** | F1 (Priority 1) |
| **Trạng thái ban đầu** | F0 là Leader, F1-F7 là Followers |

## Cấu Hình Mạng Ban Đầu

```
Cluster: [F0(Leader), F1, F2, F3, F4, F5, F6, F7]
Priority:  0          1    2    3    4    5    6    7

Tổng số nodes: 8
Majority: 8/2 + 1 = 5 nodes
```

---

## Mô Tả Sự Cố

**F1 bị mất kết nối hoàn toàn** với tất cả các nodes khác trong mạng (Leader F0 và các Followers F2-F7).

Nguyên nhân có thể:
- Network interface failure
- Firewall rules thay đổi
- Network partition cô lập F1

---

## Phân Tích Xử Lý Theo Code

### Phần 1: Phía Leader F0 và Các Followers F2-F7 (Majority Group)

#### T=0s - T=2s: Hoạt động bình thường
```
F0 (Leader) gửi heartbeat tới F1, F2, F3, F4, F5, F6, F7
- F2-F7: Nhận heartbeat thành công → cập nhật lastHeartbeat
- F1: KHÔNG nhận được (bị cô lập)
```

#### T=2s: F0 phát hiện F1 unreachable

**Code path**: `heartbeat.go:sendHeartbeat()` → `BroadcastMessageWithFailureHandler()` → `leaderOnSendFailure()`

```
F0 gửi heartbeat tới F1 → THẤT BẠI (network unreachable)
↓
leaderOnSendFailure(F1) được gọi:
  1. Membership.MarkDead(F1)     → F1 bị đánh dấu DEAD trong view của F0
  2. broadcastMembershipView()  → Gửi membership update tới F2-F7
```

**Kết quả**:
- F0 membership view: `[F0(alive), F1(DEAD), F2(alive), ..., F7(alive)]`
- F2-F7 nhận membership update → đồng bộ: `F1 = DEAD`

#### T=2s trở đi: Cluster tiếp tục hoạt động bình thường

```
Cluster còn lại: [F0(Leader), F2, F3, F4, F5, F6, F7] = 7 nodes
- F0 vẫn là Leader
- F0 gửi heartbeat tới F2-F7 (bỏ qua F1 vì đã DEAD)
- Hệ thống hoạt động bình thường với 7 nodes
```

---

### Phần 2: Phía F1 (Isolated Node)

#### T=0s - T=5s: F1 không nhận được heartbeat

```
F1 lastHeartbeat = T=0s (lần cuối nhận từ F0)
F1 chờ heartbeat từ F0...
```

#### T=5s: F1 phát hiện heartbeat timeout

**Code path**: `heartbeat.go:checkHeartbeat()` → `selectNewLeader()`

```
checkHeartbeat():
  time.Since(lastHeartbeat) = 5s > HeartbeatTimeout (5s)
  leaderID = F0 (có leader)
  → Gọi selectNewLeader()
```

#### T=5s: F1 thực hiện selectNewLeader()

**Code path**: `leader.go:selectNewLeader()`

```
selectNewLeader():
  1. state = Follower ✓ → tiếp tục
  2. Membership.MarkDead(F0)  → F0 bị đánh dấu DEAD trong view của F1
  3. GetHighestPriorityAliveNode():
     - F1 view: [F0(DEAD), F1(alive), F2(alive), ..., F7(alive)]
     - Highest priority alive = F1 (priority 1)
  4. F1 == highestPriority → sendIAmNewLeaderAndWaitForAcks()
```

#### T=5s: F1 gửi I AM NEW LEADER

**Code path**: `leader.go:sendIAmNewLeaderAndWaitForAcks()`

```
sendIAmNewLeaderAndWaitForAcks():
  1. state = ClaimingLeader
  2. currentTerm++
  3. Broadcast MsgIAmNewLeader tới F0, F2-F7
     → TẤT CẢ ĐỀU THẤT BẠI (F1 bị cô lập)
  4. Chạy waitForLeaderClaimAcks()
```

#### T=5s - T=10s: F1 chờ ACK

**Code path**: `leader.go:waitForLeaderClaimAcks()`

```
waitForLeaderClaimAcks():
  yesCount = 1 (bản thân F1)
  aliveCount = 7 (F1, F2-F7 trong view của F1, F0 đã DEAD)
  majority = 7/2 + 1 = 4

  Chờ ACK từ F2-F7...
  → KHÔNG NHẬN ĐƯỢC ACK NÀO (F1 bị cô lập)

  Sau HeartbeatTimeout (5s):
    yesCount = 1 < majority = 4
    → finishClaim() với yesCount < majority
```

#### T=10s: F1 claim thất bại

**Code path**: `leader.go:finishClaim()`

```
finishClaim():
  yesCount = 1 < majority = 4
  → state = Follower (quay về Follower)
  → Log: "Leader claim failed: YES=1 < majority=4"
```

#### T=10s trở đi: F1 lặp lại chu kỳ

```
F1 state = Follower
F1 không nhận heartbeat từ ai
↓
Sau 5s → timeout → selectNewLeader() → claim → thất bại → Follower
↓
Lặp lại vô hạn...
```

---

## Timeline Tổng Hợp

```
T=0s     : Cluster bình thường, F0 là Leader
           F1 bị mất kết nối (network isolation)

T=2s     : F0 gửi heartbeat → phát hiện F1 unreachable
           F0: MarkDead(F1) + broadcast membership update
           F2-F7: Nhận update, đồng bộ F1=DEAD

T=2s+    : Majority group [F0, F2-F7] tiếp tục hoạt động bình thường

T=5s     : F1 timeout → selectNewLeader()
           F1: MarkDead(F0) → F1 là highest priority → claim leadership

T=5-10s  : F1 gửi MsgIAmNewLeader (thất bại, không ai nhận)
           F1 chờ ACK...

T=10s    : F1 claim thất bại (yesCount=1 < majority=4)
           F1 quay về Follower

T=15s    : F1 lại timeout → claim → thất bại → Follower

...      : F1 lặp chu kỳ claim thất bại mỗi 10 giây
```

---

## Trạng Thái Cuối Cùng

### Majority Group (F0, F2-F7)

| Node | State | Leader View | Membership View |
|------|-------|-------------|-----------------|
| F0 | **Leader** | F0 | F1=DEAD, others=alive |
| F2 | Follower | F0 | F1=DEAD, others=alive |
| F3 | Follower | F0 | F1=DEAD, others=alive |
| F4 | Follower | F0 | F1=DEAD, others=alive |
| F5 | Follower | F0 | F1=DEAD, others=alive |
| F6 | Follower | F0 | F1=DEAD, others=alive |
| F7 | Follower | F0 | F1=DEAD, others=alive |

**Kết quả**: Hoạt động bình thường với 7 nodes

### Isolated Node (F1)

| Node | State | Leader View | Membership View |
|------|-------|-------------|-----------------|
| F1 | Follower (cycling) | None/F0(dead) | F0=DEAD, F1=alive, others=alive |

**Kết quả**: F1 bị cô lập, liên tục cố claim leadership và thất bại

---

## Kết Luận

### ✅ CODE COVER THÀNH CÔNG

| Tiêu chí | Kết quả | Giải thích |
|----------|---------|------------|
| **Majority group ổn định** | ✅ PASS | F0 vẫn là Leader, cluster 7 nodes hoạt động bình thường |
| **Không split-brain** | ✅ PASS | F1 không thể lên Leader vì không đủ majority ACK |
| **Leader detection** | ✅ PASS | F0 phát hiện F1 unreachable và loại khỏi cluster |
| **Membership sync** | ✅ PASS | F2-F7 đồng bộ membership view (F1=DEAD) |
| **Claim protection** | ✅ PASS | F1 claim thất bại do không đủ majority (1 < 4) |

### Phân Tích Chi Tiết

1. **Majority requirement hoạt động đúng**:
   - F1 chỉ có 1 vote (bản thân) < majority (4)
   - Không thể tự phong Leader

2. **Leader F0 xử lý đúng**:
   - Phát hiện F1 unreachable qua `leaderOnSendFailure()`
   - Đánh dấu F1 dead và broadcast membership

3. **Cluster resilience**:
   - Mất 1 node (F1) không ảnh hưởng hoạt động
   - 7 nodes còn lại > majority (5) → cluster vẫn functional

4. **Khi F1 recover**:
   - F1 cần gửi `MsgMembershipRequest` để rejoin
   - Leader F0 sẽ cập nhật membership và chấp nhận F1

---

## Ghi Chú Bổ Sung

### Vấn đề tiềm ẩn

1. **F1 cycling**: F1 liên tục claim và thất bại (mỗi ~10s), tiêu tốn tài nguyên
   - **Giải pháp tiềm năng**: Thêm exponential backoff cho claim retry

2. **Membership view không đồng bộ**: F1 có view khác với majority
   - **Không ảnh hưởng**: Vì F1 bị cô lập, không thể gây split-brain

### Recovery scenario

Khi F1 khôi phục kết nối:
```
F1 nhận heartbeat từ F0
→ handleHeartbeat(): cập nhật currentLeaderID = F0
→ F1 quay về Follower bình thường
→ F1 gửi MembershipRequest để đồng bộ membership view
```
