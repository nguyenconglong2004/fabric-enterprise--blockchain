# Heartbeat trong Ordering Service (Raft)

## Tổng quan

Heartbeat là cơ chế trung tâm để duy trì tính ổn định của cụm Raft. Leader định kỳ gửi tin hiệu heartbeat tới tất cả các follower. Khi follower không nhận được heartbeat trong giới hạn thời gian cho phép, nó khởi động quy trình bầu chọn leader mới.

**Các hằng số thời gian** (định nghĩa trong [source/internal/network/protocol.go](../source/internal/network/protocol.go)):

| Hằng số | Giá trị | Ý nghĩa |
|---|---|---|
| `HeartbeatInterval` | 2 giây | Leader gửi heartbeat nếu không có block nào được gửi trong khoảng này |
| `HeartbeatTimeout` | 5 giây | Follower chờ tối đa trước khi coi leader đã chết |
| Leader claim timeout | 10 giây (2×HBT) | Thời gian tối đa để node ClaimingLeader được công nhận |
| Expected leader deadline | 15 giây (3×HBT) | Thời gian chờ leader mới gửi `I AM NEW LEADER` |

---

## Luồng hoạt động chính

### 1. Vòng lặp giám sát (`monitorHeartbeat`)

File: [source/internal/raft/heartbeat.go:15](../source/internal/raft/heartbeat.go#L15)

Một goroutine chạy liên tục với ticker 1 giây. Mỗi tick gọi `checkHeartbeat()`.

```
monitorHeartbeat()
└── ticker (1s)
    └── checkHeartbeat()
```

---

### 2. Kiểm tra và gửi heartbeat (`checkHeartbeat`)

File: [source/internal/raft/heartbeat.go:30](../source/internal/raft/heartbeat.go#L30)

Hàm này có ba nhánh tùy theo trạng thái hiện tại của node:

```
checkHeartbeat()
├── [Leader]
│   └── Nếu time.Since(lastBlockSentTime) >= 2s
│       └── sendHeartbeat()
│
├── [ClaimingLeader]
│   └── Không làm gì (bỏ qua để tránh gọi selectNewLeader)
│
└── [Follower / Syncing]
    ├── Nếu expectedLeaderID != "" && now > expectedLeaderDeadline
    │   └── Đánh dấu chết expectedLeaderID → selectNewLeader()
    └── Nếu time.Since(lastHeartbeat) > 5s && leaderID != ""
        └── selectNewLeader()
```

**Lưu ý quan trọng:** Block proposal và block commit cũng cập nhật `lastBlockSentTime` (leader) và `lastHeartbeat` (follower), do đó khi hệ thống đang xử lý block thường xuyên, heartbeat riêng biệt có thể không cần gửi.

---

### 3. Gửi heartbeat (`sendHeartbeat`)

File: [source/internal/raft/heartbeat.go:81](../source/internal/raft/heartbeat.go#L81)

Leader xây dựng và gửi một `MsgHeartbeat` tới tất cả thành viên còn sống (trừ bản thân):

```go
msg := types.Message{
    Type:      types.MsgHeartbeat,
    Term:      currentTerm,
    SenderID:  <leaderPeerID>,
    Timestamp: time.Now(),
}
```

Mỗi lần gửi chạy trong goroutine riêng. Nếu gửi thất bại → gọi `leaderOnSendFailure(peerID)` để đánh dấu node đó là chết và broadcast membership mới.

---

### 4. Nhận heartbeat (`handleHeartbeat`)

File: [source/internal/raft/heartbeat.go:240](../source/internal/raft/heartbeat.go#L240)

Follower nhận `MsgHeartbeat` và xử lý theo các bước:

```
handleHeartbeat(msg)
├── msg.Term < currentTerm?
│   └── Heartbeat cũ (stale) → gửi HeartbeatResponse để thông báo leader lỗi thời
│
├── Tính gap = time.Since(lastHeartbeat)
│   └── gap > 10s (2×HeartbeatTimeout) && lastHeartbeat != zero → rejoinDetected = true
│
├── Cập nhật: lastHeartbeat = now()
│
├── leaderID != self?
│   └── Nếu self đang là Leader hoặc ClaimingLeader → step down về Follower
│
├── Cập nhật: currentLeaderID, currentTerm
│
├── Xóa: expectedLeaderID, expectedLeaderDeadline (đã có leader thật)
│
└── rejoinDetected && state == Follower?
    └── go StartSync("rejoin-after-disconnect")
```

---

### 5. Heartbeat từ term cũ — Phát hiện stale leader

Khi follower nhận heartbeat có `term < currentTerm`, nó gửi `HeartbeatResponse` chứa thông tin term hiện tại và leaderID thực.

File: [source/internal/raft/heartbeat.go:145](../source/internal/raft/heartbeat.go#L145)

```go
resp := types.HeartbeatResponse{
    CurrentTerm:     currentTerm,
    CurrentLeaderID: leaderID.String(),
    MembershipData:  serializeMembershipView(),
}
```

Khi leader nhận `HeartbeatResponse` có term cao hơn (xử lý tại [heartbeat.go:172](../source/internal/raft/heartbeat.go#L172)):

```
handleHeartbeatResponse(msg)
├── Chỉ xử lý nếu state == Leader hoặc ClaimingLeader
├── resp.CurrentTerm <= curTerm? → bỏ qua
├── Tìm leaderID hợp lệ trong response
└── Step down:
    ├── state = Follower
    ├── currentTerm = resp.CurrentTerm
    ├── currentLeaderID = leaderID mới
    ├── Xóa expectedLeaderID, expectedLeaderDeadline
    ├── updateMembershipFromData() + MarkAlive(self)
    ├── go requestMembershipJoin(leaderID) → thông báo leader ta còn sống
    └── go StartSync("stepped-down-after-stale-heartbeat")
```

---

### 6. Block message hoạt động như heartbeat

File: [source/internal/raft/transaction.go](../source/internal/raft/transaction.go)

Để tránh gửi thừa tin hiệu khi hệ thống bận, các message liên quan đến block cũng reset bộ đếm heartbeat:

| Sự kiện | Phía nào | Cập nhật |
|---|---|---|
| Leader gửi block proposal | Leader | `lastBlockSentTime = now()` |
| Leader gửi block commit | Leader | `lastBlockSentTime = now()` |
| Follower nhận block proposal | Follower | `updateLastHeartbeat()` |
| Follower nhận block commit | Follower | `updateLastHeartbeat()` |

---

## Sơ đồ luồng tổng thể

```
                         ┌──────────────────────────────────────────┐
                         │               LEADER                      │
                         │                                           │
                         │  monitorHeartbeat() [ticker 1s]          │
                         │       │                                   │
                         │  checkHeartbeat()                         │
                         │       │                                   │
                         │  time.Since(lastBlockSentTime) >= 2s?    │
                         │       │ YES                               │
                         │  sendHeartbeat()                          │
                         │       │                                   │
                         │  [loop all alive members]                 │
                         │       │                                   │
                         └───────┼───────────────────────────────────┘
                                 │ MsgHeartbeat (term, senderID)
                          ───────┼────────────────────────
                                 ▼
                         ┌──────────────────────────────────────────┐
                         │              FOLLOWER                     │
                         │                                           │
                         │  handleHeartbeat(msg)                    │
                         │       │                                   │
                         │  msg.Term < currentTerm? ────YES──►  sendHeartbeatResponse()
                         │       │ NO                           (MsgHeartbeatResponse)
                         │  gap > 10s? → rejoinDetected              │
                         │       │                             ◄─────┘
                         │  reset lastHeartbeat                      │
                         │  update currentLeaderID, term             │
                         │       │                                   │
                         │  rejoinDetected? ────YES──► StartSync()  │
                         └──────────────────────────────────────────┘

                         ┌──────────────────────────────────────────┐
                         │            FOLLOWER (timeout)             │
                         │                                           │
                         │  checkHeartbeat()                         │
                         │  time.Since(lastHeartbeat) > 5s?         │
                         │       │ YES                               │
                         │  selectNewLeader()                        │
                         └──────────────────────────────────────────┘
```

---

## Xử lý lỗi và các tình huống đặc biệt

### Gửi heartbeat thất bại

File: [source/internal/raft/leader.go](../source/internal/raft/leader.go) — `leaderOnSendFailure`

Khi `SendMessage()` trả về lỗi trong goroutine gửi heartbeat:
1. Kiểm tra node vẫn là Leader; nếu không → bỏ qua.
2. `Membership.MarkDead(peerID)` — đánh dấu peer là chết.
3. `broadcastMembershipView()` — phát broadcast membership mới tới các node còn lại.

### Phát hiện rejoin sau mất kết nối

Điều kiện: `gap > 2 × HeartbeatTimeout` (tức 10 giây) tính từ lần heartbeat cuối.

Khi follower nhận heartbeat đầu tiên sau khoảng trống dài:
- `rejoinDetected = true`
- Gọi `StartSync("rejoin-after-disconnect")` để tải lại các block đã bỏ lỡ.

Quá trình sync ([source/internal/raft/sync.go](../source/internal/raft/sync.go)):
1. Broadcast `SyncStatusRequest` để thu thập `commitIndex` và hash từ các node.
2. Chọn mục tiêu đồng thuận (majority agree).
3. Tải song song theo shard (`SyncShardSize = 64` block/shard).
4. Xác minh tính liên tục của chuỗi hash.
5. Cài đặt block và log entries.

### Nhiều leader cùng lúc (split-brain)

Khi một node đang là Leader nhận heartbeat từ leader khác có term >= currentTerm:
- Node đó lập tức step down về Follower.
- Cập nhật `currentLeaderID` và `currentTerm` từ heartbeat nhận được.

### Expected leader không phản hồi

Sau khi `selectNewLeader()` chọn node ưu tiên cao nhất không phải bản thân:
- Đặt `expectedLeaderID` và `expectedLeaderDeadline = now() + 15s`.
- Nếu hết hạn mà không nhận được `I AM NEW LEADER`:
  - `Membership.MarkDead(expectedLeaderID)`
  - Gọi lại `selectNewLeader()` để chọn ưu tiên kế tiếp.

---

## Tính năng giả lập độ trễ mạng (Testing)

File: [source/internal/raft/heartbeat.go:125](../source/internal/raft/heartbeat.go#L125) — `SetHeartbeatDelay`

Cho phép giả lập mạng bị chậm/đứt tới các node cụ thể trong quá trình kiểm thử:

```
SetHeartbeatDelay(priorities=[]int{1}, duration=30s)
    → heartbeat tới node có priority=1 sẽ bị bỏ qua trong 30 giây
    → mô phỏng cô lập mạng (network partition)
```

**Cơ chế:**
- `delayedPriorities map[int]bool` — tập priority bị trì hoãn.
- `heartbeatPausedUntil time.Time` — thời điểm hết hiệu lực.
- Khi `heartbeatPausedUntil` đã qua, `delayedPriorities` tự động xóa và heartbeat gửi bình thường trở lại.

Có thể kích hoạt qua CLI:
```
delay <seconds> <priority1> [priority2] ...
# Ví dụ: delay 30 1
```

---

## Cấu trúc dữ liệu liên quan

### Trường trong `RaftNode` (node.go)

| Trường | Kiểu | Mô tả |
|---|---|---|
| `lastHeartbeat` | `time.Time` | Thời điểm nhận heartbeat/block gần nhất (follower) |
| `lastBlockSentTime` | `time.Time` | Thời điểm gửi block gần nhất (leader) |
| `expectedLeaderID` | `peer.ID` | Node đang chờ làm leader mới |
| `expectedLeaderDeadline` | `time.Time` | Hạn cuối cho expected leader |
| `delayedPriorities` | `map[int]bool` | Set priority bị delay heartbeat (testing) |
| `heartbeatPausedUntil` | `time.Time` | Thời điểm hết hiệu lực của delay |

### Loại message (types/message.go)

| Loại | Hướng | Mục đích |
|---|---|---|
| `MsgHeartbeat` | Leader → Follower | Tín hiệu leader vẫn sống |
| `MsgHeartbeatResponse` | Follower → Stale leader | Thông báo leader lỗi thời về term cao hơn |

### `HeartbeatResponse` struct

```go
type HeartbeatResponse struct {
    CurrentTerm     int64
    CurrentLeaderID string
    MembershipData  map[string]interface{}
}
```

---

## Bảng tóm tắt các tình huống lỗi

| Tình huống | Phát hiện bằng | Hành động |
|---|---|---|
| Leader crash | Follower: `lastHeartbeat` quá 5s | `selectNewLeader()` |
| Expected leader không phản hồi | `expectedLeaderDeadline` hết hạn | MarkDead + `selectNewLeader()` lại |
| Mất kết nối dài rồi rejoin | Gap > 10s khi nhận heartbeat | `StartSync("rejoin-after-disconnect")` |
| Leader lỗi thời (stale) | Follower có term cao hơn | Gửi `HeartbeatResponse` → leader step down |
| Hai leader cùng tồn tại | Leader nhận heartbeat từ leader khác | Step down ngay lập tức |
| Gửi heartbeat thất bại | `SendMessage()` trả lỗi | `MarkDead(peer)` + broadcast membership |

---

## Đồng bộ hóa và an toàn luồng

- `mu` (RWMutex): bảo vệ `state`, `currentTerm`, `currentLeaderID`, `lastHeartbeat`, `expectedLeaderID`.
- `delayMu` (Mutex): bảo vệ `delayedPriorities`, `heartbeatPausedUntil`.
- `syncMu` (Mutex): đảm bảo `StartSync()` không chạy đồng thời nhiều lần.
- Mỗi lần gửi heartbeat chạy trong goroutine riêng — không block vòng lặp chính.
- `sendHeartbeatResponse()` và `requestMembershipJoin()` cũng được gọi qua `go` để tránh deadlock.
