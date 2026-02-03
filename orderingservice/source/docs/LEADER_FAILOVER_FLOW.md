# Quá trình Thay thế Leader (Leader Failover)

Tài liệu này mô tả chi tiết quá trình thay thế leader trong hệ thống Raft hiện tại.

---

## Tổng quan

Hệ thống sử dụng **Hybrid Priority-Based Raft** - một cơ chế thay thế leader dựa trên priority với xác nhận từ majority. Khi follower phát hiện leader timeout:

1. **Đánh dấu leader cũ là dead** trong membership view
2. **Tìm follower có priority cao nhất** (dựa trên join time)
3. **Nếu là node có priority cao nhất**: Gửi `MsgIAmNewLeader`, chờ majority ACK rồi mới lên leader
4. **Nếu không**: Đặt `expectedLeaderID`, chờ node đó gửi `MsgIAmNewLeader`

---

## Các Trạng Thái Node

| Trạng Thái | Mô Tả |
|------------|-------|
| `Follower` | Node bình thường, nhận heartbeat từ leader |
| `Leader` | Leader hiện tại, gửi heartbeat mỗi 2 giây |
| `ClaimingLeader` | Đang tuyên bố làm leader, chờ ACK từ đa số |

**File**: `internal/types/state.go`

---

## Các Message Types Liên Quan

| Message Type | Sender | Receiver | Mục đích |
|-------------|--------|----------|----------|
| `MsgHeartbeat` | Leader | All Followers | Xác nhận leader còn sống |
| `MsgIAmNewLeader` | Candidate | All | Tuyên bố làm leader mới |
| `MsgLeaderClaimAck` | Follower | Candidate | Chấp nhận (YES) / Từ chối (NO) claim |

**Cấu trúc dữ liệu:**
- `IAmNewLeaderClaim`: chứa `NewLeaderID`, `NewTerm`, `Priority`
- `LeaderClaimAckData`: chứa `Accept` (bool), `Term`

**File**: `internal/types/message.go`

---

## Cấu Hình Timeout

| Tham số | Giá trị | Mô tả |
|---------|---------|-------|
| `HeartbeatInterval` | 2 giây | Leader gửi heartbeat định kỳ |
| `HeartbeatTimeout` | 5 giây | Thời gian chờ tối đa trước khi coi leader đã chết |

**File**: `internal/network/protocol.go`

---

## Flow Chi Tiết

### Bước 1: Leader gửi Heartbeat (Bình thường)

- Leader gửi `MsgHeartbeat` tới tất cả followers mỗi 2 giây
- Followers cập nhật `lastHeartbeat` khi nhận được
- Nếu gửi tới follower thất bại → leader đánh dấu follower đó là dead

**File**: `internal/raft/heartbeat.go` → `sendHeartbeat()`

---

### Bước 2: Follower Phát Hiện Heartbeat Timeout

**Điều kiện trigger:**
- `time.Since(lastHeartbeat) > HeartbeatTimeout` (5 giây)
- Có leader hiện tại (`leaderID != ""`)

**Khi nào xảy ra:**
- Leader thực sự chết (crash)
- Network partition
- Network chậm tạm thời (false positive)

**File**: `internal/raft/heartbeat.go` → `checkHeartbeat()`

---

### Bước 3: Chọn Leader Mới (selectNewLeader)

1. **Kiểm tra state**: Chỉ xử lý nếu đang là `Follower` hoặc `ClaimingLeader`
2. **Đánh dấu leader cũ là dead**: `Membership.MarkDead(oldLeaderID)`
3. **Tìm node có priority cao nhất**: `GetHighestPriorityAliveNode()`
   - Priority = join order (node join sớm hơn = priority số nhỏ hơn = ưu tiên cao hơn)
4. **Quyết định**:
   - **Nếu chính mình có priority cao nhất** → Gọi `sendIAmNewLeaderAndWaitForAcks()`
   - **Nếu không** → Đặt `expectedLeaderID` và chờ

**File**: `internal/raft/leader.go` → `selectNewLeader()`

---

### Bước 4a: Node có Priority Cao Nhất - Gửi I AM NEW LEADER

1. **Double-check**: Đảm bảo mình vẫn có priority cao nhất
2. **Chuyển state**: `state = ClaimingLeader`
3. **Tăng term**: `currentTerm++`
4. **Broadcast**: Gửi `MsgIAmNewLeader` tới tất cả nodes
5. **Chờ ACK**: Chạy goroutine `waitForLeaderClaimAcks()`

**File**: `internal/raft/leader.go` → `sendIAmNewLeaderAndWaitForAcks()`

---

### Bước 4b: Chờ và Xử Lý ACK

1. **Khởi tạo**: `yesCount = 1` (bản thân = YES), tính `majority = N/2 + 1`
2. **Chờ tối đa**: `HeartbeatTimeout` (5 giây)
3. **Đếm YES**: Mỗi khi nhận ACK với `Accept = true`, tăng `yesCount`
4. **Kết thúc khi**:
   - Đủ majority YES → Trở thành Leader, bắt đầu gửi heartbeat
   - Hết timeout mà không đủ → Quay về Follower

**File**: `internal/raft/leader.go` → `waitForLeaderClaimAcks()`, `finishClaim()`

---

### Bước 4c: Các Node Khác - Xử Lý I AM NEW LEADER

**Logic chấp nhận (YES):**
1. **Nếu đang chờ expected leader**: Chỉ YES nếu claimer đúng là expected leader
2. **Nếu không có expected**: Chỉ YES nếu claimer là highest priority trong view của mình

**Sau khi nhận claim hợp lệ:**
- Cập nhật `currentLeaderID = claimer`
- Cập nhật `currentTerm = newTerm`
- Clear `expectedLeaderID`
- Gửi `MsgLeaderClaimAck` với `Accept = true/false`

**File**: `internal/raft/leader.go` → `handleIAmNewLeader()`

---

### Bước 5: Xử Lý Khi Expected Leader Không Claim

Nếu đang chờ expected leader mà hết `HeartbeatTimeout`:
1. Đánh dấu expected leader là dead
2. Clear expected leader info
3. Gọi lại `selectNewLeader()` → node có priority kế tiếp sẽ được chọn

**File**: `internal/raft/heartbeat.go` → `checkHeartbeat()`

---

### Bước 6: Leader Mới Gửi Heartbeat

Sau khi trở thành leader (nhận đủ majority YES):
- Chuyển `state = Leader`
- Bắt đầu gửi heartbeat ngay lập tức
- Followers nhận heartbeat → confirm leader mới, clear `expectedLeaderID`

**File**: `internal/raft/heartbeat.go` → `handleHeartbeat()`

---

## Sơ Đồ Tổng Hợp

```
                    ┌─────────────────┐
                    │  Leader Crash   │
                    └────────┬────────┘
                             │
                             ▼
              ┌──────────────────────────────┐
              │  Tất cả Follower phát hiện   │
              │  timeout sau 5 giây          │
              └──────────────┬───────────────┘
                             │
                             ▼
              ┌──────────────────────────────┐
              │  Mỗi node gọi selectNewLeader│
              │  và xác định highest priority│
              └──────────────┬───────────────┘
                             │
            ┌────────────────┴────────────────┐
            │                                 │
            ▼                                 ▼
   ┌─────────────────────┐          ┌─────────────────────┐
   │ Node có priority    │          │ Các node khác       │
   │ cao nhất            │          │                     │
   ├─────────────────────┤          ├─────────────────────┤
   │ state=ClaimingLeader│          │ expectedLeaderID=X  │
   │ term++              │          │ term++              │
   │ Broadcast           │          │ Chờ I AM NEW LEADER │
   │ MsgIAmNewLeader     │          │ từ expected leader  │
   └──────────┬──────────┘          └──────────┬──────────┘
              │                                 │
              │         ┌───────────────────────┘
              │         │
              ▼         ▼
   ┌──────────────────────────────┐
   │  Followers nhận claim        │
   │  → Kiểm tra: claimer có đúng │
   │    là expected/highest?      │
   │  → Gửi ACK (YES/NO)          │
   └──────────────┬───────────────┘
                  │
                  ▼
   ┌──────────────────────────────┐
   │  Claimer đếm ACK             │
   │  - Đủ majority YES → Leader  │
   │  - Không đủ → Follower       │
   └──────────────┬───────────────┘
                  │
                  ▼
   ┌──────────────────────────────┐
   │  Leader mới gửi heartbeat    │
   │  → Cluster ổn định           │
   └──────────────────────────────┘
```

---

## Ví Dụ Timeline

### Scenario 1: Leader A chết, Follower B có priority cao nhất

```
Cluster: [A(priority=0, LEADER), B(priority=1), C(priority=2), D(priority=3)]

T=0s     : Leader A gửi heartbeat (term 5)
T=2s     : Leader A gửi heartbeat (term 5)
T=4s     : Leader A CRASH!
T=7s     : B, C, D phát hiện timeout → gọi selectNewLeader()

─────────── FAILOVER BẮT ĐẦU ───────────

T=7.00s  :
  • Node B: Mình là highest priority → Broadcast MsgIAmNewLeader, state=ClaimingLeader
  • Node C, D: expectedLeaderID = B, chờ B claim

T=7.05s  : C, D nhận claim từ B → Gửi ACK YES

T=7.10s  : B nhận 2 ACK YES → yesCount=3 >= majority=2 → state=Leader

T=7.15s  : B gửi heartbeat → C, D confirm B là leader

─────────── FAILOVER HOÀN TẤT ───────────

Tổng thời gian: ~0.1-0.2 giây
```

---

### Scenario 2: Expected Leader Cũng Chết

```
Cluster: [A(priority=0, LEADER), B(priority=1), C(priority=2), D(priority=3)]

T=7.00s  : A chết → B gửi MsgIAmNewLeader, C/D đặt expectedLeaderID=B
T=7.02s  : B cũng CRASH!
T=12.00s : C, D timeout chờ B → Đánh dấu B DEAD → selectNewLeader()

─────────── RETRY FAILOVER ───────────

T=12.00s :
  • Node C: Mình là highest priority → Broadcast MsgIAmNewLeader
  • Node D: expectedLeaderID = C

T=12.05s : D nhận claim từ C → Gửi ACK YES
T=12.10s : C nhận ACK → majority đủ → C trở thành Leader mới
```

---

## Đặc Điểm Của Cơ Chế

### Ưu Điểm

| Đặc điểm | Mô tả |
|----------|-------|
| **Failover nhanh** | ~0.1-0.2 giây (không cần voting phức tạp) |
| **Deterministic** | Leader mới luôn là node có priority cao nhất còn sống |
| **Có xác nhận** | Cần majority ACK trước khi lên leader (tránh split-brain) |
| **Retry tự động** | Nếu expected leader không claim, tự động chọn node tiếp theo |

### Nhược Điểm / Rủi Ro

| Rủi ro | Mô tả |
|--------|-------|
| **Split-brain** | Có thể xảy ra nếu network partition tạo 2 nhóm, mỗi nhóm có majority riêng |
| **Membership desync** | Các nodes có membership view khác nhau có thể chọn expected leader khác nhau |
| **False positive** | Network chậm có thể gây timeout sai |
| **Term desync** | Mỗi node tự tăng term riêng khi đặt expectedLeader, có thể không đồng bộ |

---

## So Sánh Với Raft Truyền Thống

| Khía cạnh | Raft truyền thống | Cơ chế hiện tại |
|-----------|-------------------|-----------------|
| **Leader election** | Vote-based, cần majority | Priority-based + majority ACK |
| **Candidate** | Bất kỳ node nào timeout trước | Chỉ node có priority cao nhất |
| **Term tăng** | Chỉ candidate tăng | Candidate + followers đều tăng |
| **Failover time** | ~1-2 giây (election timeout ngẫu nhiên) | ~0.1-0.2 giây |
| **Split-brain protection** | Có (quorum voting) | Có (majority ACK) |
| **Complexity** | Cao | Trung bình |

---

## File Tham Khảo

| File | Nội dung |
|------|----------|
| `internal/raft/leader.go` | Logic claim leadership và xử lý ACK |
| `internal/raft/heartbeat.go` | Monitoring heartbeat và timeout |
| `internal/raft/consensus.go` | Message routing |
| `internal/types/state.go` | Định nghĩa NodeState |
| `internal/types/message.go` | Định nghĩa MessageType và structures |
| `internal/network/protocol.go` | Cấu hình timeout |

---

## Kết Luận

Cơ chế failover hiện tại là **Hybrid Priority-Based Raft** với đặc điểm:

1. **Priority-based**: Node có priority cao nhất (join sớm nhất) sẽ claim leadership
2. **Majority ACK**: Cần đủ majority YES trước khi trở thành leader
3. **Expected leader**: Các node khác chờ node có priority cao nhất claim, có timeout để retry
4. **Fast failover**: Thời gian failover rất nhanh (~0.1-0.2 giây)

Cơ chế này phù hợp cho môi trường cần failover nhanh và có mạng tương đối ổn định.
