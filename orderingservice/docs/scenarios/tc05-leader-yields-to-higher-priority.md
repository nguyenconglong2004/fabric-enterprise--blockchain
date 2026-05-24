# TC05 — Leader không step down khi vote YES cho node priority cao hơn

## 1. Bối cảnh

| Node | Priority | Vai trò | Trạng thái |
|---|---|---|---|
| **f0** | 0 (cao nhất) | Follower (đã nhường ngôi cho f1 từ TC04) | Bình thường |
| **f1** | 1 | **Leader** | Vẫn alive, tạm dừng heartbeat tới mọi node (`delay 5 0 2`) |
| **f2** | 2 | Follower | Bình thường |

**Giả định:** Cluster 3 node (`majority = 2`). f1 đã trở thành leader ở phase trước (ví dụ từ TC04). f1 chạy `delay 5 0 2` — `sendHeartbeat()` skip priority 0 và 2 trong 5 giây. f1 vẫn xử lý inbound message bình thường (delay một chiều).

**Điểm khác biệt với TC04:**
- TC04: leader (f0, p=0) bị claim bởi follower (f1, p=1) — claimer có priority **thấp hơn**.
- **TC05: leader (f1, p=1) bị claim bởi follower (f0, p=0) — claimer có priority cao hơn.**
- Cùng kết quả mong muốn: leader hiện tại vote NO, step down thụ động qua heartbeat, claimer thắng nhờ majority còn lại.

**Ký hiệu term:** Term trước sự kiện = **T = 1** (f1 đã tăng term khi trở thành leader).

---

## 2. Luồng sự kiện (trước fix — bug)

```
Thời gian | f1 (Leader, term T)        | f0 (prio 0)                  | f2 (prio 2)
----------+----------------------------+------------------------------+---------------------
t = 0s    | Gửi HB (T) đều đặn        | lastHB cập nhật              | lastHB cập nhật
t ≈ Xs    | `delay 5 0 2`              | (chưa biết)                  | (chưa biết)
          | sendHeartbeat skip p0, p2  |                              |
t ≈ X+5s  | (delay vẫn active)         | time.Since(lastHB) > 5s      | time.Since(lastHB) > 5s
          |                            | *** TIMEOUT ***              | (timeout ~cùng lúc)
          |                            | selectNewLeader():           | Highest priority = f0 ≠ self
          |                            |  MarkDead(f1)                | → expectedLeaderID = f0
          |                            |  hp=f0(self) → claim         | → chờ I AM NEW LEADER từ f0
          |                            |  currentTerm T→T+1           |
          |                            |  Broadcast MsgIAmNewLeader   |
----------+----------------------------+------------------------------+---------------------
t ≈ X+5s  | Nhận MsgIAmNewLeader       | (chờ ACK)                    | Nhận MsgIAmNewLeader
          | evaluateAndAckLeaderClaim: |                              | (expectedID = f0 = claimer)
          |  state=Leader (bypass      |                              | → accept ngay, vote YES
          |   TC04 guard vì hp=f0=     |                              | currentTerm = T+1
          |   claimer → skip IF block) |                              | currentLeaderID = f0
          |  accept: hp==claimer ✓     |                              |
          |   newTerm>=curTerm ✓       |                              |
          |  vote YES (BUG)            |                              |
          |  currentTerm = T+1        |                              |
          |  currentLeaderID = f0     |                              |
          |  state VẪN = Leader       |                              |
----------+----------------------------+------------------------------+---------------------
t ≈ X+5s  |                            | Nhận YES từ f1, f2          |
          |                            | yesCount=3 ≥ majority(2)    |
          |                            | finishClaim():               |
          |                            |  state=Leader (term T+1)    |
          |                            |  go sendHeartbeat()          |
----------+----------------------------+------------------------------+---------------------
t ≈ X+5s  | (delay kết thúc ~cùng lúc) | Gửi HB(T+1) tới f1, f2      | Nhận HB từ f0 (term T+1)
          | checkHeartbeat:            | Nhận HB từ f1 (term T+1):   |  MarkAlive(f0)
          |  state=Leader → sendHB    |  msg.Term(T+1)<curTerm(T+1) |  currentLeaderID = f0
          |  currentTerm=T+1 (đồng    |  = false → không stale       | ✓
          |   bộ khi vote YES)         |  sender≠self, state=Leader  |
          |                            |  → STEP DOWN → Follower      |
          |                            |  currentLeaderID = f1 (SAI) |
----------+----------------------------+------------------------------+---------------------
[Mắc kẹt] state=Leader               | state=Follower               | state=Follower
          currentLeaderID=f0         | currentLeaderID=f1           | currentLeaderID=f0
          (inconsistent!)             |                              |
```

---

## 3. Phân tích bug

### Tại sao TC04 guard không chặn được

TC04 guard (`curLeader != rn.Transport.ID()`) bảo vệ block **bên trong nhánh `if hp.PeerID != claimerID`**. Khi claimer là node priority cao nhất (f0), `hp = f0 = claimer` → điều kiện `hp.PeerID != claimerID` là **false** → **toàn bộ IF block bị bỏ qua**, TC04 guard chưa bao giờ được kiểm tra.

Code rơi thẳng vào nhánh accept:

```go
// Không qua IF block khi hp == claimer
accept := false
if hp != nil && hp.PeerID == claimerID && data.NewTerm >= curTerm {
    accept = true                          // ← vào đây
    rn.mu.Lock()
    rn.currentLeaderID = claimerID        // ← cập nhật, nhưng...
    rn.currentTerm = data.NewTerm         // ← currentTerm đồng bộ với new leader
    rn.lastHeartbeat = time.Now()
    rn.mu.Unlock()
    // state VẪN = Leader ← không được thay đổi
}
```

Hậu quả: `currentTerm` của f1 đã là T+1 (đồng bộ với new leader f0). Khi delay kết thúc và `sendHeartbeat` chạy, f1 gửi heartbeat T+1. f0 (Leader T+1) nhận thấy cùng term từ sender khác → rơi vào nhánh step-down tại [heartbeat.go:252-258](../../source/internal/raft/heartbeat.go#L252-L258).

### Điều kiện trigger

- Cluster nhỏ: node priority cao nhất (f0) không phải leader hiện tại.
- f0 còn sống, chỉ không nhận được heartbeat tạm thời.
- f0 timeout và gửi claim: leader hiện tại (f1) có `hp = f0 = claimer` → skip IF block → vote YES sai.

---

## 4. Fix

Thêm early return ngay đầu [`evaluateAndAckLeaderClaim`](../../source/internal/raft/leader.go#L189): nếu node đang là `Leader` hoặc `ClaimingLeader`, luôn vote NO và return ngay — không cần đánh giá claim.

```go
func (rn *RaftNode) evaluateAndAckLeaderClaim(claimerID peer.ID, data types.IAmNewLeaderClaim) {
    // [TC04+TC05] Leader/ClaimingLeader always rejects claims from other nodes.
    // The claimer wins via majority of others; this node steps down passively
    // when it receives the new leader's heartbeat (handleHeartbeat step-down branch)
    // or a stale-term heartbeat response (handleHeartbeatResponse).
    rn.mu.RLock()
    state := rn.state
    rn.mu.RUnlock()
    if state == types.Leader || state == types.ClaimingLeader {
        rn.sendLeaderClaimAck(claimerID, data.NewTerm, false)
        return
    }
    // ... phần còn lại không đổi ...
}
```

Early return này bao quát cả TC04 (lower-priority claim) và TC05 (higher-priority claim). TC04 guard (`curLeader != rn.Transport.ID()`) trong block mark-dead trở nên không cần thiết và đã được revert về dạng TC03 gốc — đơn giản hơn vì early return đảm bảo leaders không bao giờ tới đó.

Step-down của leader cũ diễn ra thụ động:
- **Nhánh A (nhanh hơn):** New leader gửi first heartbeat (term T+1) ngay sau khi `finishClaim` → leader cũ (term T) nhận → `msg.Term ≥ currentTerm`, sender ≠ self, state==Leader → step down tại [heartbeat.go:252-258](../../source/internal/raft/heartbeat.go#L252-L258).
- **Nhánh B (backup):** Delay kết thúc, leader cũ gửi HB (term T) → new leader thấy stale → reply `MsgHeartbeatResponse{term=T+1}` → leader cũ vào [handleHeartbeatResponse](../../source/internal/raft/heartbeat.go#L164) → step down.

---

## 5. Luồng sự kiện sau fix

```
Thời gian | f1 (Leader, term T)        | f0 (prio 0)                  | f2 (prio 2)
----------+----------------------------+------------------------------+---------------------
t ≈ X+5s  | Nhận MsgIAmNewLeader       | (chờ ACK)                    | vote YES (expected path)
          | state=Leader → early return|                              |
          | vote NO                    |                              |
----------+----------------------------+------------------------------+---------------------
t ≈ X+5s  |                            | Nhận YES từ f2 + self       |
          |                            | yesCount=2 ≥ majority(2)    |
          |                            | finishClaim():               |
          |                            |  state=Leader (term T+1)    |
          |                            |  go sendHeartbeat()          |
----------+----------------------------+------------------------------+---------------------
t ≈ X+5s  | Nhận HB từ f0 (term T+1): | Gửi HB(T+1) tới f1, f2      | Nhận HB từ f0 (T+1)
          |  msg.Term(T+1)≥curTerm(T)  |                              |  currentLeaderID=f0 ✓
          |  sender≠self, state=Leader |                              |
          |  → STEP DOWN → Follower    |                              |
          |  currentLeaderID=f0 ✓     |                              |
          |  currentTerm=T+1 ✓        |                              |
----------+----------------------------+------------------------------+---------------------
[Nhất quán] state=Follower            | state=Leader                 | state=Follower
            currentLeaderID=f0        | currentLeaderID=f0           | currentLeaderID=f0
            term=T+1                  | term=T+1                     | term=T+1
```

---

## 6. Trạng thái cuối

| Node | State | Term | currentLeaderID | Ghi chú |
|---|---|---|---|---|
| f0 | **Leader** | **T+1** | **f0** | Thắng nhờ self + f2 = 2/2 majority |
| **f1** | **Follower** | **T+1** | **f0** | Step down khi nhận HB term T+1 từ f0 |
| f2 | Follower | T+1 | f0 | Ổn định theo expected-leader path |

---

## 7. Tác động tới các case khác

### TC04 (f0 leader, claim từ f1 — lower-priority)

Early return bao quát luôn TC04: f0 (Leader) nhận claim từ f1 → state==Leader → early return vote NO. TC04 guard bây giờ là dead code với Leaders nhưng vô hại; được revert về dạng gốc (không có guard `curLeader != self`). Kết quả TC04 không đổi.

### TC03 (leader crash)

f0 đã chết; f1 và f2 là Follower khi nhận claim → không vào early return → flow TC03 chạy bình thường, f1 thắng nhờ TC03 mark-dead fix → vẫn pass.

### TC01 (follower isolate)

Follower thực sự nhận claim → không vào early return → flow cũ. Không ảnh hưởng.

### TC02 (follower timeout qua expected path)

f2 dùng expected-leader path, không qua `evaluateAndAckLeaderClaim` → không ảnh hưởng.

---

## 8. Lịch sử fix

| Ngày | Mô tả |
|---|---|
| 2026-05-24 | Phát hiện qua scenario test sau TC04: f1 leader → `delay 5 0 2` → f0 không lên được leader vì f1 vote YES nhưng giữ state Leader. Fix bằng early return cho Leader/ClaimingLeader ở đầu `evaluateAndAckLeaderClaim`. Revert TC04 guard (không còn cần thiết, dead code với early return). |
