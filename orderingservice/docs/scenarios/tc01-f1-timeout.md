# TC01 — Chỉ f1 bị timeout heartbeat từ leader

## 1. Bối cảnh

| Node | Priority | Vai trò | Trạng thái mạng |
|---|---|---|---|
| **f0** | 0 (cao nhất) | **Leader** | Bình thường |
| **f1** | 1 | Follower | **Không nhận được heartbeat từ f0** |
| f2 | 2 | Follower | Bình thường |
| f3 | 3 | Follower | Bình thường |
| f4 | 4 | Follower | Bình thường |
| f5 | 5 | Follower | Bình thường |
| f6 | 6 | Follower | Bình thường |
| f7 | 7 | Follower | Bình thường |

**Giả định:** f0 vẫn còn sống và đang gửi heartbeat bình thường đến f2–f7. Chỉ có đường mạng f0 → f1 bị gián đoạn (network partition một chiều hoặc packet loss cục bộ). f0 không biết f1 gặp sự cố.

**Ký hiệu term:** Term hiện tại trước khi xảy ra sự kiện = **T**.

---

## 2. Luồng sự kiện

```
Thời gian | f0 (Leader)          | f1 (Follower)              | f2–f7 (Follower)
----------+----------------------+----------------------------+-------------------------
t = 0s    | Gửi heartbeat (T)    | lastHeartbeat cập nhật     | Nhận HB bình thường
t = 2s    | Gửi heartbeat (T)    | [mất kết nối bắt đầu]      | Nhận HB bình thường
t = 4s    | Gửi heartbeat (T)    | Không nhận được            | Nhận HB bình thường
          |                      | time.Since(lastHB) > 5s    |
t = 5s    | Gửi heartbeat (T)    | *** TIMEOUT ***            | Nhận HB bình thường
          |                      | selectNewLeader()          |
          |                      | → Mark f0 DEAD (cục bộ)    |
          |                      | → state = ClaimingLeader   |
          |                      | → currentTerm: T → T+1     |
          |                      | → Broadcast MsgIAmNewLeader|
----------+----------------------+----------------------------+-------------------------
t = 5s    | Nhận MsgIAmNewLeader | [chờ majority ACK]         | Nhận MsgIAmNewLeader
          | từ f1                |                            | từ f1
          | → hp=f0≠f1 → NO     |                            | → spawn goroutine
          | → Gửi NO ngay        |                            |   evaluateAck()
          |                      |                            |   sleep ~3s
----------+----------------------+----------------------------+-------------------------
t ≈ 8s    | Gửi heartbeat (T)    | f1 nhận HB (T):            | Goroutine thức dậy:
          |                      | T < T+1 → REJECT          | hp = f0 (vẫn alive)
          |                      | gửi HeartbeatResponse      | → Gửi NO tới f1
----------+----------------------+----------------------------+-------------------------
t = 15s   | Bình thường         | Hết 10s chờ ACK            | Bình thường
          |                      | yesCount=1 < majority=5    |
          |                      | finishClaim() thất bại:    |
          |                      |  state = Follower          |
          |                      |  currentTerm T+1 → T       |
          |                      |  currentLeaderID = ""      |
          |                      |  (lastHeartbeat giữ cũ)    |
----------+----------------------+----------------------------+-------------------------
t = 16s   | Bình thường         | checkHeartbeat tick:        | Bình thường
          |                      | leaderID="" → SKIP timeout |
          |                      | (KHÔNG re-election)        |
----------+----------------------+----------------------------+-------------------------
t = 17s   | Gửi heartbeat (T)    | handleHeartbeat(T):        | Bình thường
          |                      | T >= T → CHẤP NHẬN         |
          |                      | gap ≈ 13s > 10s            |
          |                      |  → rejoinDetected = true   |
          |                      |  → StartSync(rejoin)       |
          |                      | MarkAlive(f0)              |
          |                      | currentLeaderID = f0       |
          |                      | currentTerm = T            |
          |                      | lastHeartbeat = now()      |
----------+----------------------+----------------------------+-------------------------
t ≥ 17s   | Bình thường         | Sync block đã bỏ lỡ        | Bình thường
          |                      | Sau sync: follower bình    |
          |                      | thường                     |
```

---

## 3. Phân tích từng bước

### Bước 1 — f1 phát hiện timeout

`monitorHeartbeat()` chạy ticker 1 giây. Khi `time.Since(lastHB) > 5s && leaderID != ""`, gọi `selectNewLeader()`.

[heartbeat.go:69](../../source/internal/raft/heartbeat.go#L69)

### Bước 2 — f1 chọn leader mới

```go
// selectNewLeader() trên f1
rn.Membership.MarkDead(f0.ID)  // Đánh dấu f0 chết CỤC BỘ
highestPriority := rn.Membership.GetHighestPriorityAliveNode()
// f0 đã bị mark dead → highest alive = f1

if highestPriority.PeerID == rn.Transport.ID() {
    rn.sendIAmNewLeaderAndWaitForAcks()
}
```

[leader.go:17](../../source/internal/raft/leader.go#L17)

### Bước 3 — f1 gửi claim với term T+1

```go
rn.state = types.ClaimingLeader
rn.currentTerm++  // T → T+1
rn.BroadcastToAllMembers(MsgIAmNewLeader)
// majority = 8/2 + 1 = 5
// Chờ tối đa 10 giây
```

[leader.go:66](../../source/internal/raft/leader.go#L66)

### Bước 4 — Tất cả node từ chối claim

- **f0**: `hp = f0` (chính mình, vẫn alive) ≠ f1 → gửi NO ngay.
- **f2–f7**: vừa nhận heartbeat từ f0 → sleep ~3s chờ leader hiện tại timeout. Sau khi thức dậy, f0 vẫn alive (vẫn gửi heartbeat đều) → gửi NO.

### Bước 5 — f0 vẫn gửi heartbeat tới f1, f1 reject

```go
// handleHeartbeat() trên f1
if msg.Term < rn.currentTerm {  // T < T+1
    go rn.sendHeartbeatResponse(senderID)
    return  // không reset lastHeartbeat
}
```

[heartbeat.go:240](../../source/internal/raft/heartbeat.go#L240)

### Bước 6 — finishClaim hoàn lại state về Follower

Sau 10 giây hết chờ ACK, `finishClaim()` chạy nhánh failure:

```go
} else {
    rn.state = types.Follower
    rn.currentTerm--          // T+1 → T (hoàn lại increment)
    rn.currentLeaderID = ""   // xóa tham chiếu leader cũ đã bị mark dead
    // lastHeartbeat KHÔNG reset
    log.Printf("[%s] Leader claim failed: ... reverted to term %d", ..., rn.currentTerm)
}
```

[leader.go:146](../../source/internal/raft/leader.go#L146)

Ba thao tác trong nhánh này phối hợp giải quyết ba vấn đề tiềm ẩn:

| Thao tác | Mục đích |
|---|---|
| `currentTerm--` | Hoàn lại term về T → khớp với f0 → heartbeat của f0 sẽ được chấp nhận |
| `currentLeaderID = ""` | `checkHeartbeat()` yêu cầu `leaderID != ""` để trigger re-election → ngăn vòng lặp |
| KHÔNG reset `lastHeartbeat` | Giữ gap lớn để `rejoinDetected = true` khi nhận lại heartbeat → sync bù block bỏ lỡ |

### Bước 7 — f1 chấp nhận heartbeat tiếp theo từ f0

```go
// handleHeartbeat() trên f1 — nhận HB (term T)
// rn.currentTerm = T (đã hoàn lại)
if msg.Term < rn.currentTerm {  // T < T → FALSE → không reject
    ...
}

// Tính gap trước khi reset
gap := time.Since(rn.lastHeartbeat)  // ~13s
rejoinDetected := gap > 2*HeartbeatTimeout  // 13s > 10s → TRUE

rn.lastHeartbeat = time.Now()
rn.currentLeaderID = leaderID
rn.currentTerm = msg.Term

// Sau khi unlock mu
if leaderID != rn.Transport.ID() {
    // Khôi phục leader về alive nếu đang bị mark dead
    rn.Membership.Mu.RLock()
    info, exists := rn.Membership.Members[leaderID]
    isDead := exists && !info.IsAlive
    rn.Membership.Mu.RUnlock()
    if isDead {
        rn.Membership.MarkAlive(leaderID)
    }
}

if rejoinDetected && state == types.Follower {
    go rn.StartSync("rejoin-after-disconnect")
}
```

[heartbeat.go:240–306](../../source/internal/raft/heartbeat.go#L240)

---

## 4. Trạng thái cuối

| Node | State | Term | currentLeaderID | Membership |
|---|---|---|---|---|
| f0 | Leader | T | f0 | đủ 8 node alive |
| **f1** | **Follower** | **T** | **f0** | **đủ 8 node alive** |
| f2–f7 | Follower | T | f0 | đủ 8 node alive |

Cluster trở lại trạng thái nhất quán hoàn toàn. f1 sync bù các block đã bỏ lỡ trong giai đoạn ClaimingLeader (~10 giây) thông qua `rejoin-after-disconnect`.

---

## 5. Các cơ chế then chốt

### 5.1 Hoàn lại term khi election thất bại

Tăng `currentTerm` xảy ra trong `sendIAmNewLeaderAndWaitForAcks()`. Khi election thất bại (không đủ majority YES), `finishClaim()` giảm lại đúng một lần để hội tụ về term cũ. An toàn vì trong TC01 không có node nào vote YES (tất cả reject) → không có node nào đã cập nhật term lên T+1.

### 5.2 Xóa `currentLeaderID` để ngăn re-election

`checkHeartbeat()` chỉ gọi `selectNewLeader()` khi cả `time.Since(lastHB) > HeartbeatTimeout` VÀ `leaderID != ""`. Đặt `currentLeaderID = ""` ngắt điều kiện thứ hai → no-op cho đến khi nhận heartbeat hợp lệ.

### 5.3 Giữ `lastHeartbeat` cũ để kích hoạt sync

`rejoinDetected = gap > 2 × HeartbeatTimeout` trong `handleHeartbeat()` cho phép trigger sync khi gap đủ lớn. Vì `lastHeartbeat` không reset trong `finishClaim()`, gap khi nhận heartbeat tiếp theo từ f0 đủ lớn (>10s) → sync chạy tự động để bù block đã bỏ lỡ.

### 5.4 Khôi phục leader về alive

`selectNewLeader()` mark `oldLeaderID` là dead trong membership cục bộ. Nếu election thất bại, leader cũ (vẫn sống) cần được restore alive. `handleHeartbeat()` kiểm tra: nếu sender đang ở trạng thái dead trong membership thì gọi `MarkAlive(leaderID)`. Chỉ thực hiện khi cần thiết, tránh tăng `Version` không cần thiết.

---

## 6. Tác động lên các node khác

### f0 (Leader)

Nhận `MsgIAmNewLeader` từ f1 → xử lý nhanh, gửi NO ngay (không sleep). Tiếp tục gửi heartbeat tới f1 bình thường (TCP vẫn hoạt động). Không bị ảnh hưởng vai trò.

### f2–f7 (Follower)

Mỗi node spawn một goroutine `evaluateAndAckLeaderClaim()` khi nhận `MsgIAmNewLeader`. Goroutine sleep ~3 giây rồi gửi NO. Goroutine tự kết thúc sau khi gửi ACK — không tích lũy.

### Cluster

Vẫn xử lý transaction bình thường vì f0 và f2–f7 ở đúng term, đủ majority. Throughput không bị gián đoạn.

---

## 7. Sơ đồ tổng thể

```
                         ┌──────────────────────────────┐
                         │  f0 (Leader, term T)         │
                         │  Gửi heartbeat đều đặn       │
                         └──────────┬───────────────────┘
                                    │
                ┌───────────────────┼──────────────────────┐
                ▼                   ▼                      ▼
        ┌──────────────┐   ┌──────────────┐      ┌──────────────┐
        │ f1: timeout  │   │ f2–f7: nhận  │      │              │
        │ →ClaimingL.  │   │ heartbeat    │      │              │
        │ broadcast    │   │ bình thường  │      │              │
        │ MsgIAmNew    │   └──────────────┘      └──────────────┘
        │ (term T+1)   │
        └──────┬───────┘
               │
        ┌──────▼────────────────────────────────┐
        │ Tất cả node gửi NO (f0 là highest)    │
        └──────┬────────────────────────────────┘
               │
        ┌──────▼────────────────────────────────┐
        │ finishClaim() thất bại:               │
        │  state = Follower                     │
        │  currentTerm T+1 → T                  │
        │  currentLeaderID = ""                 │
        │  (lastHeartbeat giữ cũ)               │
        └──────┬────────────────────────────────┘
               │
        ┌──────▼────────────────────────────────┐
        │ f0 gửi heartbeat tiếp theo (term T):  │
        │  T >= T → CHẤP NHẬN                   │
        │  gap > 10s → trigger sync             │
        │  MarkAlive(f0)                        │
        │  → f1 phục hồi hoàn toàn              │
        └───────────────────────────────────────┘
```
