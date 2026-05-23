package raft

import (
	"encoding/json"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

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

// checkHeartbeat checks if heartbeat has timed out
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

		if time.Since(lastBlock) >= rn.Config.GetHeartbeatInterval() {
			rn.sendHeartbeat()
		}
		return
	}
	// Đang claim leader thì không gọi selectNewLeader
	if state == types.ClaimingLeader {
		return
	}

	// Nếu đang chờ expected leader mà hết thời gian → đánh dấu chết, chọn leader mới
	if expectedID != "" && !expectedDeadline.IsZero() && time.Now().After(expectedDeadline) {
		rn.Logger.Printf("[%s] Expected leader %s did not send I AM NEW LEADER in time, marking dead and re-electing",
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
	if time.Since(lastHB) > rn.Config.GetHeartbeatTimeout() && leaderID != "" {
		rn.Logger.Printf("[%s] Heartbeat timeout! Last heartbeat: %v ago",
			rn.Transport.ID().ShortString(), time.Since(lastHB))
		rn.selectNewLeader()
	}
}

// sendHeartbeat sends heartbeat to all followers.
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
		rn.delayedPriorities = make(map[int]bool)
		rn.heartbeatPausedUntil = time.Time{}
	}
	rn.delayMu.Unlock()

	members := rn.Membership.GetAliveMembers()
	selfID := rn.Transport.ID()
	for _, member := range members {
		if member.PeerID == selfID {
			continue
		}
		if skippedPriorities[member.Priority] {
			rn.Logger.Printf("[%s] Skipping heartbeat to priority-%d node %s (delay active)",
				selfID.ShortString(), member.Priority, member.PeerID.ShortString())
			continue
		}
		pID := member.PeerID
		go func(peerID peer.ID) {
			if err := rn.Transport.SendMessage(peerID, msg); err != nil {
				rn.leaderOnSendFailure(peerID)
			} else {
				rn.Emitter.HeartbeatSent(selfID, peerID, currentTerm)
			}
		}(pID)
	}
}

// SetHeartbeatDelay simulates a network delay to nodes with the given priorities.
func (rn *RaftNode) SetHeartbeatDelay(priorities []int, duration time.Duration) {
	rn.delayMu.Lock()
	defer rn.delayMu.Unlock()
	rn.delayedPriorities = make(map[int]bool)
	if len(priorities) == 0 {
		rn.heartbeatPausedUntil = time.Time{}
		rn.Logger.Printf("[%s] Heartbeat delay cleared", rn.Transport.ID().ShortString())
		return
	}
	for _, p := range priorities {
		rn.delayedPriorities[p] = true
	}
	rn.heartbeatPausedUntil = time.Now().Add(duration)
	rn.Logger.Printf("[%s] Heartbeat delay set for priorities %v (duration: %v)",
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
		rn.Logger.Printf("[%s] Failed to send heartbeat response to %s: %v",
			rn.Transport.ID().ShortString(), targetID.ShortString(), err)
	}
}

// handleHeartbeatResponse handles a response from a follower to a stale heartbeat.
func (rn *RaftNode) handleHeartbeatResponse(msg types.Message) {
	rn.mu.RLock()
	state := rn.state
	curTerm := rn.currentTerm
	rn.mu.RUnlock()

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

	rn.Logger.Printf("[%s] Stepping down: heartbeat response from %s shows stale term (mine=%d, current=%d, leader=%s)",
		rn.Transport.ID().ShortString(), msg.SenderID, curTerm, resp.CurrentTerm, leaderID.ShortString())

	rn.mu.Lock()
	oldState := rn.state
	rn.state = types.Follower
	rn.currentTerm = resp.CurrentTerm
	rn.currentLeaderID = leaderID
	rn.lastHeartbeat = time.Now()
	rn.expectedLeaderID = ""
	rn.expectedLeaderDeadline = time.Time{}
	rn.mu.Unlock()

	if oldState != types.Follower {
		rn.Emitter.StateChanged(rn.Transport.ID(), oldState, types.Follower)
	}

	rn.updateMembershipFromData(resp.MembershipData)
	rn.Membership.MarkAlive(rn.Transport.ID())

	go rn.requestMembershipJoin(leaderID)
	go rn.StartSync("stepped-down-after-stale-heartbeat")
}

// updateLastHeartbeat resets the heartbeat timer (called when any leader message acts as heartbeat)
func (rn *RaftNode) updateLastHeartbeat() {
	rn.mu.Lock()
	rn.lastHeartbeat = time.Now()
	rn.mu.Unlock()
}

// handleHeartbeat handles heartbeat message.
func (rn *RaftNode) handleHeartbeat(msg types.Message) {
	rn.mu.Lock()

	// Heartbeat từ term cũ — báo lại cho sender biết để step down
	if msg.Term < rn.currentTerm {
		rn.Logger.Printf("[%s] Received stale heartbeat from %s (term %d < current %d), sending response",
			rn.Transport.ID().ShortString(), msg.SenderID, msg.Term, rn.currentTerm)
		senderID, err := peer.Decode(msg.SenderID)
		rn.mu.Unlock()
		if err == nil {
			go rn.sendHeartbeatResponse(senderID)
		}
		return
	}

	gap := time.Since(rn.lastHeartbeat)
	rejoinDetected := !rn.lastHeartbeat.IsZero() && gap > 2*rn.Config.GetHeartbeatTimeout()

	rn.lastHeartbeat = time.Now()

	leaderID, err := peer.Decode(msg.SenderID)
	if err != nil {
		rn.mu.Unlock()
		return
	}

	oldState := rn.state
	if leaderID != rn.Transport.ID() {
		if rn.state == types.Leader || rn.state == types.ClaimingLeader {
			rn.Logger.Printf("[%s] Stepping down: received heartbeat from %s (term %d >= current term %d)",
				rn.Transport.ID().ShortString(), leaderID.ShortString(), msg.Term, rn.currentTerm)
			rn.state = types.Follower
		}
	}
	rn.currentLeaderID = leaderID
	rn.currentTerm = msg.Term
	rn.expectedLeaderID = ""
	rn.expectedLeaderDeadline = time.Time{}
	state := rn.state
	rn.mu.Unlock()

	// Emit state change if we stepped down
	if oldState != state {
		rn.Emitter.StateChanged(rn.Transport.ID(), oldState, state)
	}

	// Emit heartbeat received
	rn.Emitter.HeartbeatReceived(rn.Transport.ID(), leaderID)

	// Ensure leader is marked alive locally
	if leaderID != rn.Transport.ID() {
		rn.Membership.Mu.RLock()
		info, exists := rn.Membership.Members[leaderID]
		isDead := exists && !info.IsAlive
		rn.Membership.Mu.RUnlock()
		if isDead {
			rn.Membership.MarkAlive(leaderID)
			rn.Logger.Printf("[%s] Restored leader %s to alive in membership",
				rn.Transport.ID().ShortString(), leaderID.ShortString())
		}
	}

	// Trigger sync nếu vừa rejoin sau gap dài
	if rejoinDetected && state == types.Follower {
		rn.Logger.Printf("[%s] Detected rejoin after %v gap, triggering sync",
			rn.Transport.ID().ShortString(), gap)
		go rn.StartSync("rejoin-after-disconnect")
	}
}
