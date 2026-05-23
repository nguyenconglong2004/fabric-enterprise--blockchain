# TC03 — Leader crash trong cluster 3 node (race claim eval)

## 1. Bối cảnh

| Node | Priority | Vai trò | Trạng thái |
|---|---|---|---|
| **f0** | 0 (cao nhất) | **Leader** | **Bị stop hoàn toàn (Ctrl+C)** |
| **f1** | 1 | Follower | Bình thường |
| **f2** | 2 | Follower | Bình thường |

**Giả định:** Cluster chỉ có 3 node (`majority = 3/2 + 1 = 2`). f0 (leader) bị shutdown đột ngột. f1 và f2 cùng mất heartbeat từ f0 nhưng phát hiện timeout **không đồng thời** — chênh lệch nhỏ (~1s) do thời điểm nhận heartbeat cuối cùng khác nhau.

**Điểm khác biệt với TC01/TC02:**
- TC01: f1 mất kết nối nhưng f0 vẫn alive — election fail là kết quả mong muốn.
- TC02: f2 mất kết nối tạm thời, không claim — không có election.
- **TC03: f0 thực sự chết, f1 phải lên leader thành công** — nhưng race timing khiến claim fail.

**Ký hiệu term:** Term trước sự kiện = **T**.

---

## 2. Luồng sự kiện (trước fix — bug)

```
Thời gian | f0 (Leader)  | f1 (Follower, prio 1)        | f2 (Follower, prio 2)
----------+--------------+------------------------------+-------------------------
t = 0s    | Gửi HB (T)   | lastHB cập nhật              | lastHB cập nhật
t = 2s    | Stop (Ctrl+C)| lastHB ~ T-2s                | lastHB ~ T-1s (HB sau)
t = 5s    | DEAD         | time.Since(lastHB) = 5.4s    | time.Since(lastHB) = 4.4s
          |              | *** TIMEOUT ***              | (chưa timeout)
          |              | selectNewLeader():           |
          |              |  MarkDead(f0)                |
          |              |  hp = f1 (self) → IF branch  |
          |              |  currentTerm: T → T+1        |
          |              |  state = ClaimingLeader      |
          |              |  Broadcast MsgIAmNewLeader   |
----------+--------------+------------------------------+-------------------------
t = 5s    | (dead, dial  | Gửi tới f0: dial fail        | Nhận MsgIAmNewLeader (T+1)
          |  fail)       |                              | evaluateAndAckLeaderClaim():
          |              |                              |  hp = f0 (vẫn alive ở view f2)
          |              |                              |  hp ≠ claimer(f1)
          |              |                              |  remaining = lastHB+5s - now
          |              |                              |             ≈ 0.6s → sleep
          |              |                              |  sau sleep: hp recompute
          |              |                              |   ❌ f0 VẪN alive (không
          |              |                              |      ai mark dead)
          |              |                              |  hp = f0 ≠ f1 → NO
          |              |                              | sendLeaderClaimAck(false)
----------+--------------+------------------------------+-------------------------
t ≈ 6s    |              | yesCount = 1 (self only)     |
t = 15s   |              | timeout 2×HBT = 10s          |
          |              | finishClaim():               |
          |              |  yesCount(1) < majority(2)   |
          |              |  state = Follower            |
          |              |  currentTerm: T+1 → T        |
          |              |  currentLeaderID = ""        |
----------+--------------+------------------------------+-------------------------
t ≈ 6s    |              | (checkHB SKIP — leaderID="")| time.Since(lastHB) > 5s
          |              |                              | TIMEOUT
          |              |                              | selectNewLeader():
          |              |                              |  hp = f1 ≠ self → ELSE
          |              |                              |  expectedLeaderID = f1
          |              |                              |  expectedDeadline = +15s
----------+--------------+------------------------------+-------------------------
t ≈ 21s   |              | Nhận IAmNewLeader từ f2      | Hết 15s, mark f1 dead
          |              | evaluateAndAck:              | selectNewLeader lại:
          |              |  hp = f1 (self) ≠ f2 → NO    |  hp = self(f2) → IF branch
          |              | (BUG: f1 vừa fail claim,     |  Claim T+1 → f0 dial fail
          |              |  không tự nhường cho f2)     |  f1 từ chối NO
          |              |                              |  yesCount=1 < 2 → fail
----------+--------------+------------------------------+-------------------------
[Vòng lặp] f1 không claim lại (leaderID=""), f2 fail vĩnh viễn → **CLUSTER STUCK**
```

---

## 3. Phân tích bug

### Nguyên nhân gốc

Trong [`evaluateAndAckLeaderClaim`](../../source/internal/raft/leader.go#L189), sau khi `time.Sleep(remaining)` chờ leader cũ hết timeout, code recompute `hp` nhưng **không mark current leader dead** dù `lastHB` đã quá `HeartbeatTimeout`.

Hệ quả: trong khoảng thời gian rất ngắn (chênh ~1s) giữa khi f1 timeout và f2 timeout, f2 sẽ từ chối claim của f1 vì f0 vẫn alive trong view của nó. f1 chỉ có 1 vote (self) < majority = 2 → fail.

Sau khi f1 fail và state về Follower với `currentLeaderID = ""`, f1 không thể tự trigger election lại ([heartbeat.go:checkHeartbeat](../../source/internal/raft/heartbeat.go) yêu cầu `leaderID != ""`). Và khi f2 tiếp quản claim thì f1 lại từ chối (vì `hp = self`), dẫn đến cluster bị kẹt.

### Điều kiện trigger

- Cluster nhỏ (3 node), majority = 2.
- Leader chết đột ngột.
- Hai follower phát hiện timeout cách nhau < `HeartbeatTimeout` (tức < 5s — gần như luôn luôn xảy ra trong thực tế vì offset nhận HB cuối thường < 2s).

---

## 4. Fix

**Trong `evaluateAndAckLeaderClaim`** ([leader.go:189](../../source/internal/raft/leader.go#L189)): sau `time.Sleep(remaining)`, nếu current leader đã quá HBT mà chưa được mark dead, mark dead nó trước khi recompute `hp`:

```go
if hp != nil && hp.PeerID != claimerID {
    timeoutAt := lastHB.Add(rn.Config.GetHeartbeatTimeout())
    remaining := time.Until(timeoutAt)
    if remaining > 0 {
        time.Sleep(remaining)
    }
    // [FIX TC03] Sau sleep, current leader đáng lẽ đã timeout — mark dead để
    // hp được tính lại chính xác. Trước fix, follower (chưa tự timeout) giữ
    // old leader làm hp và vote NO cho claim hợp lệ.
    rn.mu.RLock()
    curLeader := rn.currentLeaderID
    curLastHB := rn.lastHeartbeat
    rn.mu.RUnlock()
    if curLeader != "" && time.Since(curLastHB) > rn.Config.GetHeartbeatTimeout() {
        rn.Membership.MarkDead(curLeader)
    }
    hp = rn.Membership.GetHighestPriorityAliveNode()
    ...
}
```

---

## 5. Luồng sự kiện sau fix

```
Thời gian | f0           | f1                           | f2
----------+--------------+------------------------------+-------------------------
t = 5s    | DEAD         | TIMEOUT, claim (T+1)         | Nhận IAmNewLeader
          |              | gửi tới f0 (dial fail)       | evaluateAndAck:
          |              | gửi tới f2 ✓                 |  hp = f0, sleep ~0.6s
----------+--------------+------------------------------+-------------------------
t ≈ 5.6s  |              | Nhận YES từ f2               | sau sleep:
          |              | yesCount = 2 ≥ majority(2)   |  time.Since(lastHB)
          |              | finishClaim():               |    = 5.6s > 5s ✓
          |              |  *** BECOME LEADER (T+1) *** |  MarkDead(f0)
          |              |  Bắt đầu gửi HB              |  hp = f1 (recompute)
          |              |                              |  hp == claimer ✓
          |              |                              |  newTerm ≥ curTerm ✓
          |              |                              |  → ACCEPT (YES)
          |              |                              |  currentLeaderID = f1
----------+--------------+------------------------------+-------------------------
[Sau đây] DEAD           | Leader, term T+1             | Follower của f1, term T+1
```

---

## 6. Trạng thái cuối

| Node | State | Term | currentLeaderID | Membership |
|---|---|---|---|---|
| f0 | DEAD | T | — | (mark dead bởi f1, f2) |
| **f1** | **Leader** | **T+1** | **f1** | {f1 alive, f2 alive, f0 dead} |
| f2 | Follower | T+1 | f1 | {f1 alive, f2 alive, f0 dead} |

Cluster phục hồi sau ~5–6 giây kể từ khi f0 chết, không cần can thiệp thủ công.

---

## 7. Tác động tới các case khác

### TC01 không bị ảnh hưởng
Trong TC01, f0 vẫn alive và vẫn gửi heartbeat tới các node khác. Khi f1 claim tới f2..f7:
- f2..f7 có `lastHB` rất mới (vừa nhận từ f0) → `remaining > 0`, sleep.
- Sau sleep ngắn (~3s), f2..f7 lại nhận tiếp HB từ f0 trong khi đang sleep — `lastHB` được cập nhật trong `handleHeartbeat`.
- Sau sleep, `time.Since(curLastHB)` < HBT → **không mark dead** f0 → hp vẫn = f0 → NO.

Điều này đảm bảo: chỉ mark dead khi leader **thực sự** đã quá HBT, không phải chỉ vì lúc bắt đầu evaluate.

### TC02 không bị ảnh hưởng
f2 trong TC02 đi qua expected leader path (ELSE branch), không gọi `evaluateAndAckLeaderClaim`.

---

## 8. Lịch sử fix

| Ngày | Mô tả |
|---|---|
| 2026-05-23 | Phát hiện qua log f0/f1/f2 khi stop leader trong cluster 3 node. Fix bằng cách mark dead current leader sau sleep trong `evaluateAndAckLeaderClaim`. |
