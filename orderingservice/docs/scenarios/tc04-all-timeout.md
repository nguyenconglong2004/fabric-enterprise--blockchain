# TC04 — Leader delay heartbeat tới tất cả follower (cluster nhỏ 3 node)

## 1. Bối cảnh

| Node | Priority | Vai trò | Trạng thái |
|---|---|---|---|
| **f0** | 0 (cao nhất) | **Leader** | Vẫn alive, nhưng tạm dừng heartbeat tới mọi follower (`delay 7 1 2`) |
| **f1** | 1 | Follower | Bình thường (không nhận được HB từ f0) |
| **f2** | 2 | Follower | Bình thường (không nhận được HB từ f0) |

**Giả định:** Cluster chỉ có 3 node (`majority = 3/2 + 1 = 2`). f0 chạy lệnh debug `delay 7 1 2` — `sendHeartbeat()` skip mọi node priority 1 & 2 trong 7 giây. f0 vẫn xử lý message inbound bình thường (delay là một chiều f0→others).

**Điểm khác biệt với TC01/TC02/TC03:**
- TC01: chỉ một follower mất kết nối, các follower còn lại vote NO → election fail là kết quả đúng.
- TC02: follower priority thấp timeout, đi qua expected-leader path.
- TC03: leader chết hoàn toàn, các follower phải bầu mới.
- **TC04: leader vẫn alive nhưng "câm" tạm thời — followers cùng timeout VÀ leader cũng nhận được claim → tình huống "leader bị challenge khi vẫn còn sống".**

**Ký hiệu term:** Term trước sự kiện = **T = 0**.

---

## 2. Luồng sự kiện (trước fix — bug)

```
Thời gian | f0 (Leader)                | f1 (prio 1)                  | f2 (prio 2)
----------+----------------------------+------------------------------+---------------------
t = 0s    | Gửi HB (T)                | lastHB cập nhật              | lastHB cập nhật
t ≈ 4s    | `delay 7 1 2`             | (chưa biết)                  | (chưa biết)
          | sendHeartbeat skip p1, p2 |                              |
t ≈ 4–9s  | Liên tục log              | Không nhận HB                | Không nhận HB
          | "Skipping heartbeat to    |                              |
          |  priority-1 ... delay     |                              |
          |  active"                   |                              |
----------+----------------------------+------------------------------+---------------------
t = 9s    | (delay vẫn active)        | time.Since(lastHB) > 5s     | time.Since(lastHB) > 5s
          |                            | *** TIMEOUT ***             | (timeout chậm hơn ~0–1s)
          |                            | selectNewLeader():           |
          |                            |  MarkDead(f0)                |
          |                            |  hp=f1(self) → IF branch     |
          |                            |  currentTerm T→T+1           |
          |                            |  state=ClaimingLeader        |
          |                            |  Broadcast MsgIAmNewLeader   |
----------+----------------------------+------------------------------+---------------------
t = 9s    | Nhận MsgIAmNewLeader      |                              | Nhận MsgIAmNewLeader
          | evaluateAndAckLeaderClaim:|                              | evaluateAndAckLeaderClaim:
          |  hp=f0(self)≠f1           |                              |  hp=f0(view)≠f1
          |  lastHB stale (leader     |                              |  remaining ~0–1s → sleep
          |   không nhận HB)           |                              |  sau sleep:
          |  remaining<0 → no sleep   |                              |   curLeader=f0, lastHB>5s
          |  [TC03 fix mark dead]:    |                              |   → MarkDead(f0)
          |   curLeader=f0=SELF       |                              |   hp recompute = f1
          |   lastHB>5s (TRUE         |                              |  hp==claimer → YES
          |    vĩnh viễn)             |                              |  currentTerm = T+1
          |   → MarkDead(f0)          |                              |  currentLeaderID = f1
          |   (TỰ MARK MÌNH DEAD)     |                              |
          |  hp recompute = f1        |                              |
          |  hp==claimer → ACCEPT     |                              |
          |  state VẪN = Leader (BUG)|                              |
          |  currentTerm = T+1        |                              |
          |  currentLeaderID = f1     |                              |
          |  Vote YES                 |                              |
----------+----------------------------+------------------------------+---------------------
t ≈ 9s    |                            | Nhận YES từ f2, từ f0       |
          |                            | yesCount = 3 ≥ majority(2)  |
          |                            | finishClaim():               |
          |                            |  state = Leader (term T+1)  |
          |                            |  go sendHeartbeat()          |
          |                            |  StartAutoProposeBlock       |
----------+----------------------------+------------------------------+---------------------
t ≈ 11s   | (delay 7s kết thúc)        | (Leader, term T+1)          | (Follower, term T+1)
          | checkHeartbeat:           |                              |
          |  state=Leader → sendHB    |                              |
          |  với currentTerm=T+1      |                              |
          |  (đã đồng bộ khi vote YES)|                              |
          |                            |                              |
          | Gửi HB(T+1) tới f1, f2    | Nhận HB từ f0 (term T+1):   | Nhận HB từ f0 (term T+1):
          |                            |  msg.Term(T+1)<curTerm(T+1) |  cùng term → accept
          |                            |  = false → không stale       |  MarkAlive(f0)
          |                            |  sender≠self, state=Leader  |  currentLeaderID = f0
          |                            |  → STEP DOWN → Follower     |  (SAI)
          |                            |  currentLeaderID = f0       |
          |                            |  (SAI)                       |
----------+----------------------------+------------------------------+---------------------
[Mắc kẹt] State=Leader               | State=Follower               | State=Follower
          currentLeaderID=f1         | currentLeaderID=f0           | currentLeaderID=f0
          (inconsistent!)             |                              |
```

---

## 3. Phân tích bug

### Nguyên nhân gốc

Trong [`evaluateAndAckLeaderClaim`](../../source/internal/raft/leader.go#L189) đoạn TC03 fix giả định **node đang đánh giá claim là Follower** — tức `currentLeaderID` luôn trỏ tới một node khác:

```go
rn.mu.RLock()
curLeader := rn.currentLeaderID
curLastHB := rn.lastHeartbeat
rn.mu.RUnlock()
if curLeader != "" && time.Since(curLastHB) > rn.Config.GetHeartbeatTimeout() {
    rn.Membership.MarkDead(curLeader)
}
```

Khi node hiện tại CHÍNH LÀ leader bị challenge (như f0 trong TC04):
- `currentLeaderID == rn.Transport.ID()` (chính nó).
- `rn.lastHeartbeat` chỉ update khi **nhận** heartbeat → Leader không bao giờ nhận → `lastHeartbeat` permanently stale.
- Điều kiện `time.Since(curLastHB) > HBT` luôn TRUE trên Leader.

Hệ quả: **leader cũ tự mark mình dead → recompute hp = claimer → vote YES**. Tệ hơn, nhánh accept không transition state — leader cũ vẫn ở state `Leader`, đồng thời cập nhật `currentTerm = data.NewTerm`. Sau khi `delay` hết, leader cũ tiếp tục gửi heartbeat **ở term mới của leader mới** → trùng term → tại [heartbeat.go:252-258](../../source/internal/raft/heartbeat.go#L252-L258) new leader nhìn thấy sender khác cùng term → tự step down.

### Điều kiện trigger

- Leader vẫn alive nhưng tạm thời mất khả năng gửi heartbeat (delay command, network jitter, GC pause dài).
- Tất cả follower priority cao timeout và một trong số đó gửi `MsgIAmNewLeader`.
- Leader cũ nhận được claim đó.

---

## 4. Fix

> **Lưu ý:** Fix ban đầu (2026-05-24) dùng guard `curLeader != self` trong mark-dead block. Sau khi phát hiện TC05 (cùng ngày), fix được tổng quát hóa thành **early return** cho Leader/ClaimingLeader — bao quát cả TC04 (lower-priority claim) và TC05 (higher-priority claim). Guard cũ được revert.

Thêm early return ngay đầu [`evaluateAndAckLeaderClaim`](../../source/internal/raft/leader.go#L189) — Leader/ClaimingLeader luôn vote NO mà không cần đánh giá:

```go
rn.mu.RLock()
state := rn.state
rn.mu.RUnlock()
if state == types.Leader || state == types.ClaimingLeader {
    rn.sendLeaderClaimAck(claimerID, data.NewTerm, false)
    return
}
```

Step-down của old leader diễn ra thụ động sau khi new leader gửi heartbeat:
- **Nhánh A:** New leader gửi first heartbeat (term T+1) → old leader (state=Leader, term T) nhận → `msg.Term ≥ currentTerm`, sender ≠ self → step down tại [heartbeat.go:252-258](../../source/internal/raft/heartbeat.go#L252-L258).
- **Nhánh B (backup):** Delay hết, old leader gửi HB term T → new leader reply stale-term → old leader vào [handleHeartbeatResponse](../../source/internal/raft/heartbeat.go#L164) → step down.

---

## 5. Luồng sự kiện sau fix

```
Thời gian | f0 (Leader)                | f1 (prio 1)                  | f2 (prio 2)
----------+----------------------------+------------------------------+---------------------
t = 9s    | Nhận MsgIAmNewLeader      | (đang chờ ACK)               | (đang sleep evaluate)
          | evaluateAndAckLeaderClaim:|                              |
          |  [EARLY RETURN]           |                              |
          |  state=Leader → vote NO   |                              |
          |  return                   |                              |
          |                            |                              | sau sleep:
          |                            |                              |  curLeader=f0
          |                            |                              |  lastHB>5s → MarkDead(f0)
          |                            |                              |  hp=f1=claimer → YES
----------+----------------------------+------------------------------+---------------------
t ≈ 9s    |                            | Nhận YES từ f2 (1+1=2)      |
          |                            | yesCount = 2 ≥ majority(2)  |
          |                            | finishClaim():               |
          |                            |  state=Leader (term T+1)    |
          |                            |  go sendHeartbeat()          |
----------+----------------------------+------------------------------+---------------------
t ≈ 9s    | Nhận HB từ f1 (term T+1):| Gửi HB(T+1) tới f0, f2      | Nhận HB từ f1 (term T+1):
          |  msg.Term(T+1)≥curTerm(T)|                              |  MarkAlive(f1)
          |  sender≠self, state=     |                              |  currentLeaderID = f1
          |  Leader                   |                              |
          |  → STEP DOWN → Follower  |                              |
          |  currentLeaderID = f1    |                              |
          |  currentTerm = T+1       |                              |
----------+----------------------------+------------------------------+---------------------
[Sau đây] State=Follower             | State=Leader                 | State=Follower
          currentLeaderID=f1         | currentLeaderID=f1           | currentLeaderID=f1
          currentTerm=T+1            | currentTerm=T+1              | currentTerm=T+1
```

Path dự phòng: nếu heartbeat đầu tiên của f1 tới f0 bị mất, t≈11s delay hết → f0 (vẫn Leader, term T) gửi HB term T → f1 (term T+1) thấy `msg.Term(T)<curTerm(T+1)` → stale → reply `MsgHeartbeatResponse{CurrentTerm=T+1, CurrentLeaderID=f1}` → f0 vào [handleHeartbeatResponse:184-214](../../source/internal/raft/heartbeat.go#L184-L214) thấy `resp.CurrentTerm > curTerm` → step down qua path stale-term.

---

## 6. Trạng thái cuối

| Node | State | Term | currentLeaderID | Membership |
|---|---|---|---|---|
| f0 | **Follower** | **T+1** | **f1** | {f0 alive, f1 alive, f2 alive} |
| **f1** | **Leader** | **T+1** | **f1** | {f0 alive, f1 alive, f2 alive} |
| f2 | Follower | T+1 | f1 | {f0 alive, f1 alive, f2 alive} |

Cluster phục hồi nhất quán. f0 nhường ngôi cho f1 dù vẫn còn sống — đúng theo kỳ vọng của test (delay = mô phỏng leader mất khả năng phục vụ).

---

## 7. Tác động tới các case khác

### TC01 (1 follower isolate trong cluster 8 node)

Trước fix TC04/TC05, f0 (Leader) trong TC01 cũng có rủi ro tự mark mình dead khi nhận claim từ f1. Fix TC04/TC05 đảm bảo f0 vote NO ngay lập tức qua early return — đúng với mô tả "f0 vote NO" trong [tc01-f1-timeout.md](tc01-f1-timeout.md#bước-4--tất-cả-node-từ-chối-claim).

### TC02 (follower priority thấp timeout)

f2 đi qua expected-leader path ([leader.go:170-183](../../source/internal/raft/leader.go#L170-L183)), không gọi `evaluateAndAckLeaderClaim` → không ảnh hưởng.

### TC03 (leader crash trong cluster 3 node)

Leader f0 đã chết, không node nào ở trạng thái Leader khi xử lý claim → early return không kích hoạt. f1, f2 là Follower → flow TC03 mark-dead chạy bình thường → claim của f1 vẫn thành công.

### TC05 (leader bị claim từ node priority cao hơn)

Early return cũng bảo vệ trường hợp claimer có priority **cao hơn** leader hiện tại — xem [tc05-leader-yields-to-higher-priority.md](tc05-leader-yields-to-higher-priority.md).

---

## 8. Lịch sử fix

| Ngày | Mô tả |
|---|---|
| 2026-05-24 | Phát hiện qua log `delay 7 1 2` trên cluster 3 node: f0 vote YES sai cho claim của f1 do tự mark mình dead, sau đó tiếp tục gửi heartbeat ở term mới → kéo f1 step down. |
| 2026-05-24 | Fix generalized: thay guard `curLeader != self` bằng early return cho Leader/ClaimingLeader ở đầu `evaluateAndAckLeaderClaim` — bao quát cả trường hợp claimer priority cao hơn (TC05). |
