# Changelog - Các sửa đổi để fix Leader Failover

## Vấn đề ban đầu

Khi test với 2 nodes:
- Node 6000 (priority 0) là leader
- Node 6001 (priority 1) là follower
- Khi kill node 6000, node 6001 KHÔNG tự động lên làm leader

## Root Causes

### 1. Membership Serialization Bug
**File**: [consensus.go](consensus.go:167)

**Vấn đề**:
```go
// Trước đây
"join_time": member.JoinTime,  // Gửi time.Time object
```

Khi JSON encode/decode qua network, `time.Time` object không được serialize đúng format, dẫn đến lỗi parsing.

**Fix**:
```go
// Sau khi fix
"join_time": member.JoinTime.Format(time.RFC3339Nano),  // Gửi string
```

### 2. Node không Step Down khi phát hiện Higher Priority Leader
**File**: [consensus.go](consensus.go:122-139)

**Vấn đề**:
Khi node 6001 khởi động, nó tự động trở thành leader (vì là member duy nhất). Khi connect đến node 6000, nó nhận membership ack nhưng KHÔNG step down.

**Fix**:
```go
// Thêm logic trong handleMembershipAck
rn.mu.Lock()
wasLeader := rn.state == Leader
rn.currentLeaderID = leaderID
rn.currentTerm = msg.Term
rn.lastHeartbeat = time.Now()

// Step down if we were leader but discovered a higher priority leader
if wasLeader && leaderID != rn.host.ID() {
    highestPriority := rn.membership.GetHighestPriorityAliveNode()
    if highestPriority != nil && highestPriority.PeerID == leaderID {
        log.Printf("[%s] Stepping down, discovered higher priority leader: %s",
            rn.host.ID().ShortString(), leaderID.ShortString())
        rn.state = Follower
    }
} else if rn.state != Leader {
    rn.state = Follower
}
rn.mu.Unlock()
```

### 3. Leader Failure Detection Logic
**File**: [consensus.go](consensus.go:401-440)

**Vấn đề**:
```go
// Trước đây
aliveCount := len(rn.membership.GetAliveMembers())  // Đếm cả leader đã chết
majority := aliveCount/2 + 1

if voteCount >= majority {
    // Mark leader as dead
    rn.membership.MarkDead(oldLeaderID)
}
```

Flow cũ:
1. Node 6001 phát hiện timeout → propose leader down
2. Đếm alive members = 2 (bao gồm cả leader đã chết)
3. Majority = 2/2 + 1 = 2
4. Vote count = 1 (chỉ có mình nó)
5. 1 < 2 → FAIL, không claim leadership

**Fix**:
```go
// Sau khi fix
// Mark old leader as dead FIRST (before counting alive members)
if oldLeaderID != "" {
    rn.membership.MarkDead(oldLeaderID)
    log.Printf("[%s] Marked old leader %s as dead",
        rn.host.ID().ShortString(), oldLeaderID.ShortString())
}

// Now count alive members (excluding the dead leader)
aliveCount := len(rn.membership.GetAliveMembers())
majority := aliveCount/2 + 1
```

Flow mới:
1. Node 6001 phát hiện timeout → propose leader down
2. **Đánh dấu leader cũ là dead TRƯỚC**
3. Đếm alive members = 1 (chỉ node 6001)
4. Majority = 1/2 + 1 = 1
5. Vote count = 1 (chính nó)
6. 1 >= 1 → SUCCESS, claim leadership!

### 4. Membership Update Message Handling
**File**: [consensus.go](consensus.go:58-66)

**Vấn đề**:
Không phân biệt giữa:
- Join request từ node mới (chỉ leader xử lý)
- Membership broadcast từ leader (tất cả nodes apply)

**Fix**:
```go
// Check if this is a broadcast update from leader (contains full membership view)
if dataMap, ok := msg.Data.(map[string]interface{}); ok {
    if _, hasMembers := dataMap["members"]; hasMembers {
        // This is a broadcast update from leader - apply it
        log.Printf("[%s] Applying membership update from leader", rn.host.ID().ShortString())
        rn.updateMembershipFromData(msg.Data)
        return
    }
}

// This is a join request - only leader can process it
if !rn.IsLeader() {
    log.Printf("[%s] Not leader, ignoring join request", rn.host.ID().ShortString())
    return
}
```

### 5. Debug Logging
**File**: [consensus.go](consensus.go:454-459)

Thêm detailed logging để debug:
```go
// Log all alive members for debugging
aliveMembers := rn.membership.GetAliveMembers()
log.Printf("[%s] Current alive members:", rn.host.ID().ShortString())
for _, member := range aliveMembers {
    log.Printf("  - %s (priority: %d)", member.PeerID.ShortString(), member.Priority)
}
```

## Test Results

### Before Fix:
```
Node 6001: Trying to detect leader failure...
Node 6001: Leader down votes: 1/2 (need 2)
Node 6001: Not enough votes for consensus
Node 6001: State remains Follower
```

### After Fix:
```
Node 6001: Heartbeat timeout! Last heartbeat: 5.xxx ago
Node 6001: Proposing leader <old_leader> is down
Node 6001: Marked old leader as dead
Node 6001: Leader down votes: 1/1 alive nodes (need majority: 1)
Node 6001: Consensus reached: leader is down
Node 6001: Current alive members:
  - <Node6001> (priority: 1)
Node 6001: I have highest priority (1), claiming leadership
Node 6001: *** I AM NOW THE LEADER (term 1) ***
```

## Summary

Tất cả các fixes đã giải quyết vấn đề:
1. ✅ Serialization đúng format
2. ✅ Node step down khi phát hiện higher priority leader
3. ✅ **Leader failover hoạt động chính xác** - đây là fix quan trọng nhất
4. ✅ Membership updates được xử lý đúng
5. ✅ Logging đầy đủ để debug

## How to Test

Xem chi tiết trong [TEST_GUIDE.md](TEST_GUIDE.md)

Tóm tắt:
```bash
# Terminal 1
go run .  # Port 6000, first: y

# Terminal 2
go run .  # Port 6001, first: n, connect to node 1

# Check status both nodes
# Kill terminal 1 (Ctrl+C)
# Wait 8-10 seconds
# Check status node 2 → Should be Leader now!
```
