package raft

import (
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

	// If we're the leader, send heartbeats
	if state == types.Leader {
		rn.sendHeartbeat()
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
// Khi gửi tới một follower thất bại (unreachable), leader đánh dấu follower đó chết và broadcast membership tới các node còn lại.
func (rn *RaftNode) sendHeartbeat() {
	msg := types.Message{
		Type:      types.MsgHeartbeat,
		Term:      rn.currentTerm,
		SenderID:  rn.Transport.ID().String(),
		Timestamp: time.Now(),
	}

	rn.BroadcastMessageWithFailureHandler(msg, rn.leaderOnSendFailure)
}

// handleHeartbeat handles heartbeat message
func (rn *RaftNode) handleHeartbeat(msg types.Message) {
	rn.mu.Lock()
	rn.lastHeartbeat = time.Now()

	// Update leader info if term is newer
	if msg.Term >= rn.currentTerm {
		leaderID, err := peer.Decode(msg.SenderID)
		if err == nil {
			rn.currentLeaderID = leaderID
			rn.currentTerm = msg.Term
			// Đã có leader thật (heartbeat) -> bỏ chờ expected leader
			rn.expectedLeaderID = ""
			rn.expectedLeaderDeadline = time.Time{}
		}
	}
	rn.mu.Unlock()
}
