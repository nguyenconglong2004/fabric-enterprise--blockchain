package raft

import (
	"encoding/json"
	"log"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"raft-order-service/internal/network"
	"raft-order-service/internal/types"
)

// monitorHeartbeat monitors heartbeat from leader
func (rn *RaftNode) monitorHeartbeat() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rn.stopChan:
			return
		case <-ticker.C:
			rn.checkHeartbeat()
		}
	}
}

// checkHeartbeat checks if heartbeat has timed out; hoặc hết thời gian chờ expected leader gửi I AM NEW LEADER
func (rn *RaftNode) checkHeartbeat() {
	rn.mu.RLock()
	state := rn.state
	lastHB := rn.lastHeartbeat
	leaderID := rn.currentLeaderID
	expectedID := rn.expectedLeaderID
	expectedDeadline := rn.expectedLeaderDeadline
	rn.mu.RUnlock()

	// If we're the leader, send heartbeat only when no block message was sent recently.
	if state == types.Leader {
		rn.mu.RLock()
		lastBlock := rn.lastBlockSentTime
		rn.mu.RUnlock()

		if time.Since(lastBlock) >= network.HeartbeatInterval {
			rn.sendHeartbeat()
		}
		return
	}
	// Đang claim leader thì không gọi selectNewLeader
	if state == types.ClaimingLeader {
		return
	}

	// Nếu đang chờ expected leader gửi I AM NEW LEADER mà hết thời gian (= HeartbeatTimeout) -> đánh dấu chết, chọn leader mới (priority kế tiếp)
	if expectedID != "" && !expectedDeadline.IsZero() && time.Now().After(expectedDeadline) {
		log.Printf("[%s] Expected leader %s did not send I AM NEW LEADER in time, marking dead and re-electing",
			rn.Transport.ID().ShortString(), expectedID.ShortString())
		rn.Membership.MarkDead(expectedID)
		rn.mu.Lock()
		rn.expectedLeaderID = ""
		rn.expectedLeaderDeadline = time.Time{}
		rn.mu.Unlock()
		rn.selectNewLeader()
		return
	}

	// Check if heartbeat from current leader has timed out
	if time.Since(lastHB) > network.HeartbeatTimeout && leaderID != "" {
		log.Printf("[%s] Heartbeat timeout! Last heartbeat: %v ago",
			rn.Transport.ID().ShortString(), time.Since(lastHB))
		rn.selectNewLeader()
	}
}

// sendHeartbeat sends heartbeat to all followers.
// Nếu delayedPriorities còn hiệu lực (heartbeatPausedUntil chưa hết), heartbeat tới các node
// có priority trong danh sách đó bị bỏ qua — giả lập delay mạng.
// Khi heartbeatPausedUntil đã qua, delayedPriorities được xóa và gửi bình thường.
// Khi gửi tới một follower thất bại, leader đánh dấu follower đó chết.
func (rn *RaftNode) sendHeartbeat() {
	rn.mu.RLock()
	currentTerm := rn.currentTerm
	rn.mu.RUnlock()

	msg := types.Message{
		Type:      types.MsgHeartbeat,
		Term:      currentTerm,
		SenderID:  rn.Transport.ID().String(),
		Timestamp: time.Now(),
	}

	// Snapshot delay state
	rn.delayMu.Lock()
	stillPaused := time.Now().Before(rn.heartbeatPausedUntil)
	var skippedPriorities map[int]bool
	if stillPaused && len(rn.delayedPriorities) > 0 {
		skippedPriorities = rn.delayedPriorities
	} else if !stillPaused && len(rn.delayedPriorities) > 0 {
		// Pause expired — clear and send to everyone normally
		rn.delayedPriorities = make(map[int]bool)
		rn.heartbeatPausedUntil = time.Time{}
	}
	rn.delayMu.Unlock()

	members := rn.Membership.GetAliveMembers()
	for _, member := range members {
		if member.PeerID == rn.Transport.ID() {
			continue
		}
		if skippedPriorities[member.Priority] {
			log.Printf("[%s] Skipping heartbeat to priority-%d node %s (delay active)",
				rn.Transport.ID().ShortString(), member.Priority, member.PeerID.ShortString())
			continue
		}
		pID := member.PeerID
		go func(peerID peer.ID) {
			if err := rn.Transport.SendMessage(peerID, msg); err != nil {
				rn.leaderOnSendFailure(peerID)
			}
		}(pID)
	}
}

// SetHeartbeatDelay simulates a network delay to nodes with the given priorities.
// Heartbeats to those nodes will be skipped for the given duration.
// Call with an empty slice to cancel any active delay.
func (rn *RaftNode) SetHeartbeatDelay(priorities []int, duration time.Duration) {
	rn.delayMu.Lock()
	defer rn.delayMu.Unlock()
	rn.delayedPriorities = make(map[int]bool)
	if len(priorities) == 0 {
		rn.heartbeatPausedUntil = time.Time{}
		log.Printf("[%s] Heartbeat delay cleared", rn.Transport.ID().ShortString())
		return
	}
	for _, p := range priorities {
		rn.delayedPriorities[p] = true
	}
	rn.heartbeatPausedUntil = time.Now().Add(duration)
	log.Printf("[%s] Heartbeat delay set for priorities %v (duration: %v)",
		rn.Transport.ID().ShortString(), priorities, duration)
}

// sendHeartbeatResponse informs a stale leader of the current term, leader, and membership.
func (rn *RaftNode) sendHeartbeatResponse(targetID peer.ID) {
	rn.mu.RLock()
	currentTerm := rn.currentTerm
	leaderID := rn.currentLeaderID
	rn.mu.RUnlock()

	resp := types.HeartbeatResponse{
		CurrentTerm:     currentTerm,
		CurrentLeaderID: leaderID.String(),
		MembershipData:  rn.serializeMembershipView(),
	}
	msg := types.Message{
		Type:      types.MsgHeartbeatResponse,
		Term:      currentTerm,
		SenderID:  rn.Transport.ID().String(),
		Data:      resp,
		Timestamp: time.Now(),
	}
	if err := rn.Transport.SendMessage(targetID, msg); err != nil {
		log.Printf("[%s] Failed to send heartbeat response to %s: %v",
			rn.Transport.ID().ShortString(), targetID.ShortString(), err)
	}
}

// handleHeartbeatResponse handles a response from a follower to a stale heartbeat.
// If the response carries a higher term, this node steps down and resyncs state.
func (rn *RaftNode) handleHeartbeatResponse(msg types.Message) {
	rn.mu.RLock()
	state := rn.state
	curTerm := rn.currentTerm
	rn.mu.RUnlock()

	// Only act if we think we are leader or claiming
	if state != types.Leader && state != types.ClaimingLeader {
		return
	}

	data, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}
	var resp types.HeartbeatResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}

	if resp.CurrentTerm <= curTerm {
		return
	}

	leaderID, err := peer.Decode(resp.CurrentLeaderID)
	if err != nil || leaderID == rn.Transport.ID() {
		return
	}

	log.Printf("[%s] Stepping down: heartbeat response from %s shows stale term (mine=%d, current=%d, leader=%s)",
		rn.Transport.ID().ShortString(), msg.SenderID, curTerm, resp.CurrentTerm, leaderID.ShortString())

	rn.mu.Lock()
	rn.state = types.Follower
	rn.currentTerm = resp.CurrentTerm
	rn.currentLeaderID = leaderID
	rn.lastHeartbeat = time.Now()
	rn.expectedLeaderID = ""
	rn.expectedLeaderDeadline = time.Time{}
	rn.mu.Unlock()

	// Resync membership from the response.
	// Resync membership from the response.
	// After updateMembershipFromData, self is marked dead (as seen by followers).
	// Restore ourselves as alive locally so the new leader can reach us via heartbeat.
	rn.updateMembershipFromData(resp.MembershipData)
	rn.Membership.MarkAlive(rn.Transport.ID())

	// Notify the new leader that we are alive by sending a join request.
	// The leader's membership view has us marked dead, so it won't send heartbeats
	// to us. This re-join causes the leader to MarkAlive us and resume heartbeats.
	go rn.requestMembershipJoin(leaderID)
}

// updateLastHeartbeat resets the heartbeat timer (called when any leader message acts as heartbeat)
func (rn *RaftNode) updateLastHeartbeat() {
	rn.mu.Lock()
	rn.lastHeartbeat = time.Now()
	rn.mu.Unlock()
}

// handleHeartbeat handles heartbeat message.
// Chỉ reset lastHeartbeat và cập nhật leader khi term hợp lệ (>= currentTerm).
// Heartbeat từ term thấp hơn bị bỏ qua hoàn toàn — không reset timer.
func (rn *RaftNode) handleHeartbeat(msg types.Message) {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	// Heartbeat từ term cũ — báo lại cho sender biết để step down
	if msg.Term < rn.currentTerm {
		log.Printf("[%s] Received stale heartbeat from %s (term %d < current %d), sending response",
			rn.Transport.ID().ShortString(), msg.SenderID, msg.Term, rn.currentTerm)
		senderID, err := peer.Decode(msg.SenderID)
		if err == nil {
			go rn.sendHeartbeatResponse(senderID)
		}
		return
	}

	// Term hợp lệ → reset timer và cập nhật thông tin leader
	rn.lastHeartbeat = time.Now()

	leaderID, err := peer.Decode(msg.SenderID)
	if err != nil {
		return
	}

	if leaderID != rn.Transport.ID() {
		// Step down nếu đang là Leader hoặc ClaimingLeader — có leader hợp lệ khác
		if rn.state == types.Leader || rn.state == types.ClaimingLeader {
			log.Printf("[%s] Stepping down: received heartbeat from %s (term %d >= current term %d)",
				rn.Transport.ID().ShortString(), leaderID.ShortString(), msg.Term, rn.currentTerm)
			rn.state = types.Follower
		}
	}
	rn.currentLeaderID = leaderID
	rn.currentTerm = msg.Term
	// Đã có leader thật (heartbeat) -> bỏ chờ expected leader
	rn.expectedLeaderID = ""
	rn.expectedLeaderDeadline = time.Time{}
}
