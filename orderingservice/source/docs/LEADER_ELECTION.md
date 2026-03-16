# Leader Election trong Ordering Service

Tài liệu mô tả chi tiết cơ chế bầu chọn leader được triển khai trong mạng ordering service, bao gồm cơ chế khởi tạo cluster, quy trình failover khi leader chết, và các cấu trúc dữ liệu liên quan.

---

## 1. Tổng Quan Thiết Kế

Hệ thống sử dụng **Priority-Based Leader Election với xác nhận majority** — một biến thể của Raft, trong đó quyết định "ai làm leader" không thông qua vote ngẫu nhiên mà dựa trên **thứ tự gia nhập mạng (join time)**. Node gia nhập sớm hơn sẽ có priority cao hơn và được ưu tiên làm leader.

### Triết lý thiết kế

| Raft truyền thống | Cơ chế này |
|---|---|
| Bất kỳ node nào timeout trước đều có thể ứng cử | Chỉ node có priority cao nhất mới tự gửi claim |
| Timeout ngẫu nhiên (150-300ms) để tránh xung đột | Không cần ngẫu nhiên — priority xác định rõ ràng ai claim trước |
| Mọi node vote cho ứng viên tốt nhất | Mọi node biết ai là expected leader, gửi YES/NO theo đó |
| Candidate tự tăng term | Cả candidate lẫn follower đều tăng term khi phát hiện leader chết |

---

## 2. Cấu Trúc Dữ Liệu

### 2.1. MemberInfo — Thông tin một node

```go
// internal/types/membership.go
type MemberInfo struct {
    PeerID   peer.ID
    JoinTime time.Time
    Priority int     // Số nhỏ hơn = priority cao hơn (join sớm hơn)
    IsAlive  bool
}
```

Priority được gán khi node gia nhập:

```go
func (mv *MembershipView) AddMember(peerID peer.ID, joinTime time.Time) {
    mv.Members[peerID] = &MemberInfo{
        Priority: len(mv.Members), // Node đầu tiên = 0, node thứ hai = 1, ...
        IsAlive:  true,
    }
}
```

Node gia nhập sớm nhất → `Priority = 0` → được ưu tiên làm leader.

### 2.2. NodeState — Trạng thái của node

```
Follower       → trạng thái bình thường, nhận heartbeat từ leader
ClaimingLeader → đang tuyên bố làm leader, chờ majority ACK
Leader         → leader hiện tại, gửi heartbeat định kỳ
```

### 2.3. Các message liên quan đến Leader Election

```go
// internal/types/message.go

MsgHeartbeat      // Leader → Followers: xác nhận leader còn sống
MsgIAmNewLeader   // Candidate → All: tuyên bố làm leader mới
MsgLeaderClaimAck // Follower → Candidate: chấp nhận (YES) / từ chối (NO)
```

**Payload của MsgIAmNewLeader:**
```go
type IAmNewLeaderClaim struct {
    NewLeaderID string  // peer.ID của candidate
    NewTerm     int64   // term mới sau khi tăng
    Priority    int     // priority của candidate
}
```

**Payload của MsgLeaderClaimAck:**
```go
type LeaderClaimAckData struct {
    Accept bool   // true = YES, false = NO
    Term   int64
}
```

### 2.4. Các trường trong RaftNode liên quan đến Leader Election

```go
// internal/raft/node.go
type RaftNode struct {
    mu              sync.RWMutex
    state           types.NodeState   // Follower / ClaimingLeader / Leader
    currentTerm     int64             // term hiện tại
    currentLeaderID peer.ID           // leader hiện tại

    lastHeartbeat          time.Time  // lần cuối nhận heartbeat từ leader
    lastBlockSentTime      time.Time  // lần cuối leader gửi block message (proposal/commit)

    expectedLeaderID       peer.ID    // node đang chờ gửi I AM NEW LEADER
    expectedLeaderDeadline time.Time  // deadline chờ expected leader claim

    LeaderClaimAckChan chan types.Message // buffer ACK từ followers khi đang claim
}
```

---

## 3. Tham Số Cấu Hình

```go
// internal/network/protocol.go
HeartbeatInterval = 2 * time.Second  // Leader gửi heartbeat định kỳ mỗi 2 giây
HeartbeatTimeout  = 5 * time.Second  // Sau 5 giây không nhận heartbeat → leader đã chết
```

---

## 4. Khởi Tạo Cluster

### 4.1. Node đầu tiên — tự động trở thành leader

Khi một node khởi động và không có peer nào trong membership view:

```go
// internal/raft/node.go — Start()
if len(rn.Membership.GetAliveMembers()) == 1 {
    rn.becomeLeader()
}
```

```go
// internal/raft/leader.go — becomeLeader()
func (rn *RaftNode) becomeLeader() {
    rn.state = types.Leader
    rn.currentLeaderID = rn.Transport.ID()
    go rn.sendHeartbeat()
}
```

Không cần bầu chọn — node duy nhất trong mạng tự động là leader.

### 4.2. Node mới gia nhập — cơ chế Membership

Khi node B muốn gia nhập cluster đang chạy:

```
B kết nối TCP tới A (địa chỉ bootstrap)
B gửi MsgMembershipUpdate (join request) tới A:
    { PeerID: B.ID, JoinTime: now, Version: 0 }
```

**A (leader) xử lý join request:**

```
A nhận MsgMembershipUpdate
  → Kiểm tra: đây là join request hay broadcast từ leader?
    (phân biệt bằng: có field "members" trong data không)
  → AddMember(B, joinTime)
    B.Priority = len(Members)  // số thứ tự gia nhập
  → broadcastMembershipView() tới tất cả members
  → Gửi MsgMembershipAck cho B (kèm full membership view)
```

**B nhận MsgMembershipAck:**

```
B cập nhật toàn bộ membership view từ data nhận được
B.currentLeaderID = A
B.state = Follower
B.lastHeartbeat = now
```

Sau khi join thành công, B sẽ nhận heartbeat từ A và hoạt động bình thường như follower.

---

## 5. Phát Hiện Leader Chết — Heartbeat Monitoring

### 5.1. Vòng lặp monitoring

```go
// internal/raft/heartbeat.go — monitorHeartbeat()
func (rn *RaftNode) monitorHeartbeat() {
    ticker := time.NewTicker(1 * time.Second)  // kiểm tra mỗi 1 giây
    for {
        select {
        case <-rn.stopChan:
            return
        case <-ticker.C:
            rn.checkHeartbeat()
        }
    }
}
```

### 5.2. Logic kiểm tra trong checkHeartbeat()

```
Nếu state == Leader:
    Nếu time.Since(lastBlockSentTime) >= HeartbeatInterval (2s):
        sendHeartbeat()  // gửi nếu không có block message gần đây
    return  // leader không cần kiểm tra timeout

Nếu state == ClaimingLeader:
    return  // đang trong quá trình claim, không trigger lại

Nếu expectedLeaderID != "" && now > expectedLeaderDeadline:
    // Expected leader không claim trong thời hạn → coi là chết
    MarkDead(expectedLeaderID)
    expectedLeaderID = ""
    selectNewLeader()  // chọn lại (node priority kế tiếp)
    return

Nếu time.Since(lastHeartbeat) > HeartbeatTimeout (5s) && leaderID != "":
    // Leader hiện tại không phản hồi → timeout
    selectNewLeader()
```

### 5.3. Những gì được tính như heartbeat

Leader không chỉ gửi `MsgHeartbeat` thuần túy. Bất kỳ message nào từ leader cũng reset timer `lastHeartbeat` của follower:

| Message | Vai trò | Reset lastHeartbeat? |
|---------|---------|----------------------|
| `MsgHeartbeat` | heartbeat thuần túy | Có |
| `MsgBlockProposal` | đề xuất block mới | Có |
| `MsgBlockCommit` | commit block | Có |

Leader chỉ gửi `MsgHeartbeat` khi không có block activity trong 2 giây gần đây — tránh lãng phí băng thông.

---

## 6. Quy Trình Bầu Chọn Leader Mới

### 6.1. selectNewLeader() — điểm vào

```go
// internal/raft/leader.go
func (rn *RaftNode) selectNewLeader() {
    // 1. Chỉ xử lý nếu đang là Follower hoặc ClaimingLeader
    if rn.state != types.Follower && rn.state != types.ClaimingLeader {
        return
    }

    // 2. Đánh dấu leader cũ là dead
    if oldLeaderID != "" {
        rn.Membership.MarkDead(oldLeaderID)
    }

    // 3. Tìm node alive có priority cao nhất (số nhỏ nhất)
    highestPriority = rn.Membership.GetHighestPriorityAliveNode()

    // 4. Quyết định hành động
    if highestPriority.PeerID == rn.Transport.ID() {
        // Chính mình là candidate → tự gửi claim
        sendIAmNewLeaderAndWaitForAcks()
    } else {
        // Người khác là candidate → chờ họ claim
        currentTerm++
        currentLeaderID = highestPriority.PeerID   // gán tạm
        expectedLeaderID = highestPriority.PeerID
        expectedLeaderDeadline = now + HeartbeatTimeout
        lastHeartbeat = now  // tránh gọi selectNewLeader lại ngay
    }
}
```

> Lưu ý: Khi follower đặt `expectedLeaderID`, họ cũng gán `currentLeaderID = expectedLeaderID` tạm thời và tăng `currentTerm++`. Đây là điểm khác biệt với Raft chuẩn — các follower không phải candidate cũng tăng term.

### 6.2. sendIAmNewLeaderAndWaitForAcks() — candidate gửi claim

```go
// Bước 1: Double-check vẫn là highest priority
highestPriority := rn.Membership.GetHighestPriorityAliveNode()
if highestPriority.PeerID != rn.Transport.ID() {
    return  // không còn là highest priority nữa (race condition)
}

// Bước 2: Chuyển trạng thái
state = ClaimingLeader
currentTerm++

// Bước 3: Broadcast claim tới tất cả
msg = MsgIAmNewLeader {
    NewLeaderID: self.ID,
    NewTerm:     currentTerm,
    Priority:    highestPriority.Priority,
}
BroadcastMessage(msg)

// Bước 4: Chờ ACK trong goroutine riêng
go waitForLeaderClaimAcks(currentTerm)
```

### 6.3. handleIAmNewLeader() — follower nhận claim và phản hồi

```go
// internal/raft/leader.go
func (rn *RaftNode) handleIAmNewLeader(msg types.Message) {
    claimerID = decode(data.NewLeaderID)

    accept = false

    if expectedLeaderID != "" {
        // Đang chờ expected leader → chỉ YES nếu claimer đúng là người mình chờ
        if claimerID == expectedLeaderID {
            accept = true
            currentLeaderID = claimerID
            currentTerm = data.NewTerm
            lastHeartbeat = now
            expectedLeaderID = ""
        }
    } else {
        // Không có expected (vừa claim hoặc vừa startup):
        // YES nếu claimer là highest priority AND term mới hơn
        hp := Membership.GetHighestPriorityAliveNode()
        if hp.PeerID == claimerID && data.NewTerm > currentTerm {
            accept = true
            currentLeaderID = claimerID
            currentTerm = data.NewTerm
            lastHeartbeat = now
        }
    }

    // Gửi ACK dù YES hay NO
    SendMessage(claimerID, MsgLeaderClaimAck { Accept: accept, Term: data.NewTerm })
}
```

### 6.4. waitForLeaderClaimAcks() — đếm YES và kết luận

```go
// internal/raft/leader.go
func (rn *RaftNode) waitForLeaderClaimAcks(claimTerm int64) {
    yesCount := 1  // bản thân tính như YES
    majority := len(aliveMembers)/2 + 1

    timeout := time.After(HeartbeatTimeout)  // tối đa 5 giây
    ticker  := time.NewTicker(100 * time.Millisecond)

    for {
        select {
        case <-timeout:
            finishClaim(claimTerm, yesCount, majority)
            return

        case msg := <-LeaderClaimAckChan:
            if msg.Term != claimTerm { continue }
            if ack.Accept {
                yesCount++
            }

        case <-ticker.C:
            if yesCount >= majority {
                finishClaim(claimTerm, yesCount, majority)
                return
            }
        }
    }
}
```

### 6.5. finishClaim() — kết luận bầu chọn

```go
func (rn *RaftNode) finishClaim(claimTerm int64, yesCount, majority int) {
    // Guard: kiểm tra state không thay đổi trong khi chờ
    if rn.state != ClaimingLeader || rn.currentTerm != claimTerm {
        return
    }

    if yesCount >= majority {
        state = Leader
        currentLeaderID = self
        go sendHeartbeat()  // bắt đầu gửi heartbeat ngay
    } else {
        state = Follower    // claim thất bại, quay về follower
    }
}
```

---

## 7. Xử Lý Failure Trong Quá Trình Bầu Chọn

### 7.1. Expected leader không respond (chết ngay sau khi được chọn)

```
T=0   : Leader A chết
T=5s  : B, C, D timeout → selectNewLeader()
        B là highest priority → B gửi MsgIAmNewLeader
        C, D đặt expectedLeaderID = B, expectedLeaderDeadline = T+5s

T=5.1s: B cũng CRASH!

T=10s : C, D kiểm tra deadline: now > expectedLeaderDeadline
        → MarkDead(B)
        → expectedLeaderID = ""
        → selectNewLeader() lần 2

        C là highest priority còn sống → C gửi MsgIAmNewLeader
        D đặt expectedLeaderID = C

T=10.1s: D nhận claim từ C → YES
T=10.1s: C có yesCount=2 >= majority=2 → C trở thành Leader
```

### 7.2. Candidate claim thất bại do không đủ majority

Xảy ra khi network partition: candidate và một số followers bị cô lập.

```
Cluster 5 nodes: A(dead), B(priority=1), C(priority=2), D(priority=3), E(priority=4)
Network partition: {B, C} và {D, E}

Nhóm {B, C}:
  B là highest priority → B gửi claim
  C gửi YES
  yesCount = 2 (B + C) < majority = 3
  B finishClaim: thất bại → B về Follower

Nhóm {D, E}:
  D đặt expectedLeaderID = B, chờ B
  B không claim tới được → timeout
  D MarkDead(B), selectNewLeader() → D là highest trong nhóm
  D gửi claim, E YES → yesCount = 2 < majority = 3
  D thất bại → không ai trở thành leader

→ Cluster bị split, không có leader → an toàn hơn có 2 leader
```

### 7.3. Khi mạng phục hồi — xử lý term desync

Sau khi mạng phục hồi, các node có term khác nhau. Heartbeat từ leader mới nhất mang term cao hơn sẽ được tất cả follower chấp nhận:

```go
// internal/raft/heartbeat.go — handleHeartbeat()
if msg.Term >= rn.currentTerm {
    rn.currentLeaderID = leaderID
    rn.currentTerm = msg.Term
    rn.expectedLeaderID = ""  // bỏ chờ, đã có leader thực sự
}
```

---

## 8. Phát Hiện Follower Chết (từ phía Leader)

Khi leader gửi heartbeat mà không reach được một follower:

```go
// internal/raft/leader.go — leaderOnSendFailure()
func (rn *RaftNode) leaderOnSendFailure(peerID peer.ID) {
    Membership.MarkDead(peerID)
    broadcastMembershipView()  // đồng bộ view với các node còn lại
}
```

```go
// internal/raft/heartbeat.go — sendHeartbeat()
rn.BroadcastMessageWithFailureHandler(msg, rn.leaderOnSendFailure)
```

Leader chủ động cập nhật membership view và thông báo cho các node khác khi phát hiện follower offline.

---

## 9. Luồng Message Đầy Đủ — Sơ Đồ Sequence

### Kịch bản: 3-node cluster, Leader A chết

```
          A (Leader)        B (priority=1)        C (priority=2)
              │                   │                     │
              │  ◄── Heartbeat ───┤                     │
              │  ◄── Heartbeat ───┼─────────────────────┤
              │                   │                     │
              X  (A CRASH)        │                     │
              │                   │                     │
              │            [5s timeout]          [5s timeout]
              │                   │                     │
              │            selectNewLeader()    selectNewLeader()
              │            MarkDead(A)          MarkDead(A)
              │            B=highest → claim    expectedLeaderID=B
              │                   │             expectedDeadline=now+5s
              │                   │                     │
              │        state=ClaimingLeader             │
              │        currentTerm++                    │
              │                   │                     │
              │        MsgIAmNewLeader ────────────────►│
              │        {NewLeaderID=B, NewTerm=N+1}     │
              │                   │                     │
              │                   │        handleIAmNewLeader()
              │                   │        claimer==expectedLeaderID → YES
              │                   │        currentLeaderID=B
              │                   │        currentTerm=N+1
              │                   │                     │
              │        ◄─── MsgLeaderClaimAck ──────────┤
              │             {Accept: true}               │
              │                   │                     │
              │        yesCount=2 >= majority=2          │
              │        finishClaim → state=Leader        │
              │                   │                     │
              │        MsgHeartbeat ───────────────────►│
              │                   │                     │
              │                   │        handleHeartbeat()
              │                   │        currentLeaderID=B confirmed
              │                   │        expectedLeaderID=""
```

---

## 10. Sơ Đồ Trạng Thái Node

```
                        Khởi động
                            │
                    (1 node trong cluster)
                            │
                            ▼
                    ┌───────────────┐
          ┌────────►│   Follower    │◄──────────────────┐
          │         └───────┬───────┘                   │
          │                 │                           │
          │         heartbeat timeout                   │ claim thất bại
          │         (time.Since(lastHB) > 5s)          │ (yesCount < majority)
          │                 │                           │
          │                 ▼                           │
          │         selectNewLeader()                   │
          │                 │                           │
          │      ┌──────────┴──────────┐                │
          │      │                     │                │
          │  [Mình là highest]   [Người khác là highest]│
          │      │                     │                │
          │      ▼                     ▼                │
          │  ┌──────────────┐   set expectedLeaderID    │
          │  │ClaimingLeader│   chờ MsgIAmNewLeader     │
          │  └──────┬───────┘          │                │
          │         │                  │ (timeout)      │
          │   Broadcast                │                │
          │   MsgIAmNewLeader          │ MarkDead(expected)
          │   Wait for ACKs            │ selectNewLeader() lại
          │         │                  │                │
          │    [yesCount >= majority]──┘                │
          │         │                                   │
          └─────────┼───────────────────────────────────┘
                    │ [yesCount >= majority]
                    ▼
              ┌───────────┐
              │   Leader  │
              └───────────┘
                    │
              sendHeartbeat() mỗi 2s
              (hoặc block message thay thế)
```

---

## 11. So Sánh Với Raft Chuẩn

| Khía Cạnh | Raft Chuẩn | Cơ Chế Này |
|-----------|-----------|-----------|
| **Ai có thể ứng cử?** | Bất kỳ node nào sau random timeout | Chỉ node có priority cao nhất |
| **Tránh xung đột ứng cử** | Random election timeout (150-300ms) | Không cần — deterministic |
| **Điều kiện thắng** | Majority vote | Majority YES ACK |
| **Term khi follower timeout** | Follower tự tăng term khi ứng cử | Cả candidate lẫn follower đều tăng term |
| **Thời gian failover** | 150ms - vài giây | ~100-200ms (không cần chờ random timeout) |
| **Log replication** | AppendEntries gắn liền với heartbeat | Tách biệt: BlockProposal / BlockCommit |
| **Predictability** | Leader có thể là bất kỳ ai | Leader luôn là node join sớm nhất còn sống |
| **Split-brain protection** | Có (quorum voting) | Có (majority ACK required) |

---

## 12. File Tham Khảo

| File | Nội dung |
|------|----------|
| `internal/raft/leader.go` | `selectNewLeader`, `sendIAmNewLeaderAndWaitForAcks`, `handleIAmNewLeader`, `waitForLeaderClaimAcks`, `finishClaim`, `becomeLeader` |
| `internal/raft/heartbeat.go` | `monitorHeartbeat`, `checkHeartbeat`, `sendHeartbeat`, `handleHeartbeat`, `updateLastHeartbeat` |
| `internal/raft/membership.go` | `handleMembershipUpdate`, `handleMembershipAck`, `broadcastMembershipView` |
| `internal/raft/node.go` | Định nghĩa `RaftNode`, `NewRaftNode`, `Start` |
| `internal/types/membership.go` | `MemberInfo`, `MembershipView`, `AddMember`, `MarkDead`, `GetHighestPriorityAliveNode` |
| `internal/types/message.go` | `IAmNewLeaderClaim`, `LeaderClaimAckData`, `MessageType` |
| `internal/network/protocol.go` | `HeartbeatInterval`, `HeartbeatTimeout` |
