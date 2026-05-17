# TC02 — Chỉ f2 bị timeout heartbeat từ leader

## 1. Bối cảnh

| Node | Priority | Vai trò | Trạng thái mạng |
|---|---|---|---|
| **f0** | 0 (cao nhất) | **Leader** | Bình thường |
| f1 | 1 | Follower | Bình thường |
| **f2** | 2 | Follower | **Không nhận được heartbeat từ f0** |
| f3 | 3 | Follower | Bình thường |
| ... | ... | ... | ... |
| f7 | 7 | Follower | Bình thường |

**Giả định:** Chỉ f2 mất kết nối với f0 (network partition một chiều f0→f2). f0, f1, f3–f7 đều hoạt động bình thường.

**Ký hiệu term:** Term hiện tại trước khi xảy ra sự kiện = **T**.

**Điểm khác biệt then chốt so với TC01:**
- Trong TC01, f1 là **highest-priority** alive sau khi mark f0 dead → đi thẳng vào `sendIAmNewLeaderAndWaitForAcks()` (nhánh IF).
- Trong TC02, f2 **KHÔNG** phải highest-priority (f1 mới là) → đi qua **expected leader path** (nhánh ELSE), chờ f1 claim leadership.
- f1 không biết có sự cố (vẫn nhận HB từ f0 bình thường) → không bao giờ claim.
- Sau khoảng delay, f0 tiếp tục gửi HB tới f2 và cluster phục hồi.

---

## 2. Luồng sự kiện (hành vi đúng sau khi fix)

```
Thời gian | f0 (Leader, term T)   | f2 (mất kết nối tạm)        | f1, f3–f7
----------+-----------------------+------------------------------+---------------------
t = 0s    | Gửi HB (T) đều đặn   | Nhận HB bình thường          | Nhận HB bình thường
          |                       |                              |
t = ~5s   |         (partition bắt đầu — f0 không gửi được HB tới f2)
          |                       | time.Since(lastHB) > 5s      |
          |                       | *** TIMEOUT ***              |
          |                       | selectNewLeader():           |
          |                       |  MarkDead(f0)                |
          |                       |  hp = f1 (≠ self)            |
          |                       |  → ELSE branch:              |
          |                       |    currentTerm = T (KHÔNG    |
          |                       |    tăng)                     |
          |                       |    currentLeaderID = ""      |
          |                       |    expectedLeaderID = f1     |
          |                       |    expectedDeadline = +15s   |
          |                       |    lastHeartbeat = now()     |
----------+-----------------------+------------------------------+---------------------
t = 5s–   | Tiếp tục gửi HB đều   | Chờ f1 gửi MsgIAmNewLeader  | f1 KHÔNG biết có
Xretry    | (partition còn active)|  (sẽ không đến)              | sự cố — bình thường
----------+-----------------------+------------------------------+---------------------
t = X     | Partition kết thúc    |                              |
          | f0 gửi HB (T) tới f2  |                              |
          |                       | handleHeartbeat(T):          |
          |                       |  T >= currentTerm (T) → ✓   |
          |                       |  ACCEPT                      |
          |                       |  currentLeaderID = f0        |
          |                       |  lastHB = now()              |
          |                       |  expectedLeaderID = ""       |
          |                       |  MarkAlive(f0) [TC01-D fix]  |
          |                       |  → Cluster phục hồi          |
----------+-----------------------+------------------------------+---------------------
[Sau đây] | Bình thường           | Bình thường                  | Bình thường
          |                       | leader = f0, term = T        |
          |                       | alive members: đầy đủ        |
```

---

## 3. Phân tích từng bước

### Bước 1 — f2 phát hiện timeout, đi qua expected leader path

`monitorHeartbeat()` phát hiện `time.Since(lastHB) > 5s` → gọi `selectNewLeader()`.

f2 không phải highest-priority alive sau khi mark f0 dead — f1 (priority 1) mới là. `selectNewLeader()` đi vào nhánh ELSE:

```go
// leader.go:49–62 (sau fix)
} else {
    rn.mu.Lock()
    // KHÔNG tăng currentTerm — tránh lan truyền thông tin sai qua HeartbeatResponse
    rn.currentLeaderID = ""                       // [FIX] không tuyên bố leader chưa xác nhận
    rn.expectedLeaderID = highestPriority.PeerID  // = f1
    rn.expectedLeaderDeadline = time.Now().Add(3 * network.HeartbeatTimeout)  // +15s
    rn.lastHeartbeat = time.Now()                 // tránh gọi lại ngay
    rn.mu.Unlock()
}
```

f2 nghĩ: *"Mình không phải highest, để f1 lên làm leader, mình chờ nó gửi MsgIAmNewLeader."* Quan trọng: **term của f2 vẫn là T**, `currentLeaderID = ""`.

### Bước 2 — f0 tiếp tục gửi HB, f2 chấp nhận khi partition kết thúc

Khi f0 gửi lại HB (term T) tới f2, `handleHeartbeat()` trên f2 kiểm tra:

```go
// heartbeat.go
if msg.Term < rn.currentTerm {  // T < T? → FALSE (T == T)
    // KHÔNG đi vào đây → KHÔNG reject
}
```

f2 accept HB, reset `lastHeartbeat`, đặt `currentLeaderID = f0`, xóa `expectedLeaderID`.

Fix TC01-D trong `handleHeartbeat()` kích hoạt:
```go
// Nếu f0 đang bị mark dead trong membership của f2
if isDead {
    rn.Membership.MarkAlive(leaderID)  // Restore f0
}
```

### Bước 3 — Cluster phục hồi hoàn toàn

Sau khi nhận lại HB từ f0:
- f2: `currentLeaderID = f0`, `expectedLeaderID = ""`, term = T, f0 alive trong membership.
- f0: tiếp tục gửi HB bình thường tới tất cả.
- f1, f3–f7: chưa từng biết có sự cố.

Cluster không trải qua election, không có thay đổi term.

---

## 4. Trạng thái cuối

| Node | State | Term | currentLeaderID | Membership |
|---|---|---|---|---|
| f0 | Leader | T | f0 | đủ 8 node alive |
| f1 | Follower | T | f0 | đủ 8 node alive |
| f2 | Follower | T | f0 | đủ 8 node alive |
| f3–f7 | Follower | T | f0 | đủ 8 node alive |

Tất cả node đồng bộ. Không có term divergence. Cluster fully operational.

---

## 5. So sánh với TC01

| Khía cạnh | TC01 (f1 timeout) | TC02 (f2 timeout) |
|---|---|---|
| Node timeout có là highest-priority alive sau MarkDead(f0)? | ✅ Có (f1 = priority 1) | ❌ Không (f1 mới là) |
| Đường đi trong `selectNewLeader()` | Nhánh IF — gọi trực tiếp `sendIAmNewLeader...` | Nhánh ELSE — đặt `expectedLeaderID`, chờ |
| `currentTerm` thay đổi khi timeout? | Có (khi claim, +1, rồi −1 nếu thất bại) | Không (ELSE branch không tăng term) |
| Cluster có trải qua election? | Có (f1 claim rồi fail) | Không (f2 chỉ chờ, không bao giờ claim) |
| Phục hồi khi mạng OK | Nhận lại HB → rejoinDetected → sync | Nhận lại HB → accept ngay (term khớp) |

---

## 6. Điều kiện để TC02 hành xử đúng

1. Partition kết thúc trước khi `expectedLeaderDeadline` (15s) hết — nếu không, f2 sẽ mark f1 dead và tự claim, dẫn đến election với kết quả f2 fail (f0 và tất cả node còn lại từ chối).

2. Nếu f2 tự claim và thất bại (vì partition kéo dài > 15s): f2 sẽ về Follower, term vẫn đúng (vì `currentTerm++` chỉ xảy ra trong `sendIAmNewLeaderAndWaitForAcks()` và được hoàn lại bởi `finishClaim()`), và sẽ chấp nhận lại HB từ f0 khi mạng phục hồi.

---

## 7. Lịch sử fix

### Bug đã fix — Cascade failure khi f2 timeout (nguyên nhân gốc: ELSE branch tăng term sớm)

**Trước fix**, nhánh ELSE của `selectNewLeader()` thực hiện:
```go
rn.currentTerm++                              // tăng term không có consensus
rn.currentLeaderID = highestPriority.PeerID   // tuyên bố f1 là leader chưa xác nhận
```

Khi f0 gửi lại HB (term T) sau khi partition kết thúc:
- f2 từ chối (T < T+1), gửi `HeartbeatResponse{term: T+1, leaderID: f1}`.
- f0 nhận, thấy term cao hơn và `leaderID != self` → **step down** không đúng.
- f0 không gửi HB nữa → f1, f3 cũng timeout → cascade election.
- f1 thắng election (term 1), nhưng f0 bị mark dead trong f1's view → f0 không nhận HB từ f1 → f0 isolated.

**Sau fix** (xóa `currentTerm++`, đổi `currentLeaderID = f1` → `currentLeaderID = ""`):
- f2 giữ term T và `currentLeaderID = ""`.
- f0 gửi lại HB (T): f2 accept vì T >= T → cluster phục hồi trực tiếp.
- Không có cascade, không có election không cần thiết.
