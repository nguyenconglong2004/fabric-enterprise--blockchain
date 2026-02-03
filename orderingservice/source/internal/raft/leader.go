package raft

import (
	"encoding/json"
	"log"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"raft-order-service/internal/network"
	"raft-order-service/internal/types"
)

// selectNewLeader chọn leader mới khi phát hiện heartbeat timeout.
// - Nếu là follower có priority cao nhất: gửi I AM NEW LEADER, chờ majority YES rồi mới lên leader.
// - Nếu không: chọn node priority cao nhất là leader mới (expectedLeader), chờ nó gửi I AM NEW LEADER; nếu sau HeartbeatTimeout không thấy thì đánh dấu chết và gọi lại (node priority kế tiếp sẽ gửi).
func (rn *RaftNode) selectNewLeader() {
	rn.mu.Lock()
	if rn.state != types.Follower && rn.state != types.ClaimingLeader {
		rn.mu.Unlock()
		return
	}
	oldLeaderID := rn.currentLeaderID
	rn.mu.Unlock()

	if oldLeaderID != "" {
		rn.Membership.MarkDead(oldLeaderID)
		log.Printf("[%s] Marked old leader %s as dead (heartbeat timeout)",
			rn.Transport.ID().ShortString(), oldLeaderID.ShortString())
	}

	highestPriority := rn.Membership.GetHighestPriorityAliveNode()
	if highestPriority == nil {
		log.Printf("[%s] No alive nodes found", rn.Transport.ID().ShortString())
		return
	}

	aliveMembers := rn.Membership.GetAliveMembers()
	log.Printf("[%s] Current alive members after leader death:", rn.Transport.ID().ShortString())
	for _, member := range aliveMembers {
		log.Printf("  - %s (priority: %d)", member.PeerID.ShortString(), member.Priority)
	}

	if highestPriority.PeerID == rn.Transport.ID() {
		// Tôi là follower có priority cao nhất -> gửi I AM NEW LEADER, chờ majority YES
		log.Printf("[%s] I have highest priority (%d), sending I AM NEW LEADER",
			rn.Transport.ID().ShortString(), highestPriority.Priority)
		rn.sendIAmNewLeaderAndWaitForAcks()
	} else {
		// Chọn node priority cao nhất là leader mới; chờ nó gửi I AM NEW LEADER
		log.Printf("[%s] Highest priority follower is %s (priority: %d), expecting I AM NEW LEADER from it",
			rn.Transport.ID().ShortString(),
			highestPriority.PeerID.ShortString(),
			highestPriority.Priority)
		rn.mu.Lock()
		rn.currentTerm++ // term mới khi chuyển sang expected leader
		rn.currentLeaderID = highestPriority.PeerID
		rn.expectedLeaderID = highestPriority.PeerID
		rn.expectedLeaderDeadline = time.Now().Add(network.HeartbeatTimeout)
		rn.lastHeartbeat = time.Now() // tránh gọi selectNewLeader lại ngay
		rn.mu.Unlock()
	}
}

// sendIAmNewLeaderAndWaitForAcks gửi I AM NEW LEADER tới tất cả, chờ phản hồi YES/NO; nếu > nửa YES thì lên leader.
func (rn *RaftNode) sendIAmNewLeaderAndWaitForAcks() {
	highestPriority := rn.Membership.GetHighestPriorityAliveNode()
	if highestPriority == nil || highestPriority.PeerID != rn.Transport.ID() {
		return
	}

	rn.mu.Lock()
	rn.state = types.ClaimingLeader
	rn.currentTerm++
	newTerm := rn.currentTerm
	rn.mu.Unlock()

	claim := types.IAmNewLeaderClaim{
		NewLeaderID: rn.Transport.ID().String(),
		NewTerm:     newTerm,
		Priority:    highestPriority.Priority,
	}
	msg := types.Message{
		Type:      types.MsgIAmNewLeader,
		Term:      newTerm,
		SenderID:  rn.Transport.ID().String(),
		Data:      claim,
		Timestamp: time.Now(),
	}
	rn.BroadcastMessage(msg)

	go rn.waitForLeaderClaimAcks(newTerm)
}

// waitForLeaderClaimAcks chờ tối đa HeartbeatTimeout; đếm YES; nếu >= majority thì becomeLeader, không thì về Follower.
func (rn *RaftNode) waitForLeaderClaimAcks(claimTerm int64) {
	yesCount := 1 // bản thân coi như YES
	aliveCount := len(rn.Membership.GetAliveMembers())
	majority := aliveCount/2 + 1

	timeout := time.After(network.HeartbeatTimeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			rn.finishClaim(claimTerm, yesCount, majority)
			return
		case m := <-rn.LeaderClaimAckChan:
			if m.Term != claimTerm {
				continue
			}
			data, err := rn.parseLeaderClaimAckData(m.Data)
			if err == nil && data.Accept {
				yesCount++
				log.Printf("[%s] Leader claim ack YES from %s (total YES: %d, majority: %d)",
					rn.Transport.ID().ShortString(), m.SenderID, yesCount, majority)
			}
		case <-ticker.C:
			if yesCount >= majority {
				rn.finishClaim(claimTerm, yesCount, majority)
				return
			}
		}
	}
}

func (rn *RaftNode) finishClaim(claimTerm int64, yesCount, majority int) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	if rn.state != types.ClaimingLeader || rn.currentTerm != claimTerm {
		return
	}
	if yesCount >= majority {
		rn.state = types.Leader
		rn.currentLeaderID = rn.Transport.ID()
		log.Printf("[%s] *** I AM NOW THE LEADER (term %d) *** YES=%d >= majority=%d",
			rn.Transport.ID().ShortString(), claimTerm, yesCount, majority)
		go rn.sendHeartbeat()
	} else {
		rn.state = types.Follower
		log.Printf("[%s] Leader claim failed: YES=%d < majority=%d",
			rn.Transport.ID().ShortString(), yesCount, majority)
	}
}

// handleIAmNewLeader xử lý message I AM NEW LEADER.
// Trả lời YES nếu công nhận sender là leader mới (đúng là expected leader hoặc đúng là highest priority); NO nếu không.
func (rn *RaftNode) handleIAmNewLeader(msg types.Message) {
	data, err := rn.parseIAmNewLeaderData(msg.Data)
	if err != nil {
		return
	}
	claimerID, err := peer.Decode(data.NewLeaderID)
	if err != nil {
		return
	}

	rn.mu.RLock()
	expectedID := rn.expectedLeaderID
	rn.mu.RUnlock()

	accept := false
	if expectedID != "" {
		// Đang chờ I AM NEW LEADER từ expected leader
		if claimerID == expectedID {
			accept = true
			rn.mu.Lock()
			rn.currentLeaderID = claimerID
			rn.currentTerm = data.NewTerm
			rn.lastHeartbeat = time.Now()
			rn.expectedLeaderID = ""
			rn.expectedLeaderDeadline = time.Time{}
			rn.mu.Unlock()
		}
	} else {
		// Không có expected (có thể đang claim hoặc vừa timeout): chỉ YES nếu claimer là highest priority trong view
		hp := rn.Membership.GetHighestPriorityAliveNode()
		rn.mu.RLock()
		curTerm := rn.currentTerm
		rn.mu.RUnlock()
		if hp != nil && hp.PeerID == claimerID && data.NewTerm > curTerm {
			accept = true
			rn.mu.Lock()
			rn.currentLeaderID = claimerID
			rn.currentTerm = data.NewTerm
			rn.lastHeartbeat = time.Now()
			rn.mu.Unlock()
		}
	}

	ackData := types.LeaderClaimAckData{Accept: accept, Term: data.NewTerm}
	ackMsg := types.Message{
		Type:      types.MsgLeaderClaimAck,
		Term:      data.NewTerm,
		SenderID:  rn.Transport.ID().String(),
		Data:      ackData,
		Timestamp: time.Now(),
	}
	if err := rn.Transport.SendMessage(claimerID, ackMsg); err != nil {
		log.Printf("[%s] Error sending leader claim ack: %v", rn.Transport.ID().ShortString(), err)
	}
	log.Printf("[%s] Responded %v to I AM NEW LEADER from %s",
		rn.Transport.ID().ShortString(), accept, claimerID.ShortString())
}

func (rn *RaftNode) parseIAmNewLeaderData(data interface{}) (types.IAmNewLeaderClaim, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return types.IAmNewLeaderClaim{}, err
	}
	var c types.IAmNewLeaderClaim
	err = json.Unmarshal(raw, &c)
	return c, err
}

func (rn *RaftNode) parseLeaderClaimAckData(data interface{}) (types.LeaderClaimAckData, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return types.LeaderClaimAckData{}, err
	}
	var c types.LeaderClaimAckData
	err = json.Unmarshal(raw, &c)
	return c, err
}

// handleLeaderClaimAck chuyển ack vào channel cho waitForLeaderClaimAcks xử lý
func (rn *RaftNode) handleLeaderClaimAck(msg types.Message) {
	select {
	case rn.LeaderClaimAckChan <- msg:
	default:
		log.Printf("[%s] Leader claim ack channel full, dropping ack from %s",
			rn.Transport.ID().ShortString(), msg.SenderID)
	}
}

// leaderOnSendFailure được gọi khi leader gửi message (heartbeat) tới một peer thất bại.
// Leader đánh dấu peer đó chết và broadcast membership view tới các node còn lại để đồng bộ.
func (rn *RaftNode) leaderOnSendFailure(peerID peer.ID) {
	if !rn.IsLeader() {
		return
	}
	rn.Membership.MarkDead(peerID)
	log.Printf("[%s] Follower %s unreachable, marking dead and broadcasting updated membership",
		rn.Transport.ID().ShortString(), peerID.ShortString())
	rn.broadcastMembershipView()
}

// becomeLeader chuyển node sang Leader và bắt đầu gửi heartbeat (dùng khi chỉ có 1 node)
func (rn *RaftNode) becomeLeader() {
	rn.mu.Lock()
	rn.state = types.Leader
	rn.currentLeaderID = rn.Transport.ID()
	rn.mu.Unlock()
	log.Printf("[%s] *** I AM NOW THE LEADER (term %d) ***",
		rn.Transport.ID().ShortString(), rn.currentTerm)
	go rn.sendHeartbeat()
}
