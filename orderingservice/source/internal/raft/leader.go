package raft

import (
	"encoding/json"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"raft-order-service/internal/types"
)

// selectNewLeader selects a new leader when heartbeat timeout is detected.
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
		rn.Logger.Printf("[%s] Marked old leader %s as dead (heartbeat timeout)",
			rn.Transport.ID().ShortString(), oldLeaderID.ShortString())
	}

	highestPriority := rn.Membership.GetHighestPriorityAliveNode()
	if highestPriority == nil {
		rn.Logger.Printf("[%s] No alive nodes found", rn.Transport.ID().ShortString())
		return
	}

	aliveMembers := rn.Membership.GetAliveMembers()
	rn.Logger.Printf("[%s] Current alive members after leader death:", rn.Transport.ID().ShortString())
	for _, member := range aliveMembers {
		rn.Logger.Printf("  - %s (priority: %d)", member.PeerID.ShortString(), member.Priority)
	}

	if highestPriority.PeerID == rn.Transport.ID() {
		rn.Logger.Printf("[%s] I have highest priority (%d), sending I AM NEW LEADER",
			rn.Transport.ID().ShortString(), highestPriority.Priority)
		rn.sendIAmNewLeaderAndWaitForAcks()
	} else {
		rn.Logger.Printf("[%s] Highest priority follower is %s (priority: %d), expecting I AM NEW LEADER from it",
			rn.Transport.ID().ShortString(),
			highestPriority.PeerID.ShortString(),
			highestPriority.Priority)
		rn.mu.Lock()
		rn.currentLeaderID = ""
		rn.expectedLeaderID = highestPriority.PeerID
		rn.expectedLeaderDeadline = time.Now().Add(3 * rn.Config.GetHeartbeatTimeout())
		rn.lastHeartbeat = time.Now()
		rn.mu.Unlock()
	}
}

// sendIAmNewLeaderAndWaitForAcks sends I AM NEW LEADER to all, waits for ACKs.
func (rn *RaftNode) sendIAmNewLeaderAndWaitForAcks() {
	highestPriority := rn.Membership.GetHighestPriorityAliveNode()
	if highestPriority == nil || highestPriority.PeerID != rn.Transport.ID() {
		return
	}

	rn.mu.Lock()
	oldState := rn.state
	rn.state = types.ClaimingLeader
	rn.currentTerm++
	newTerm := rn.currentTerm
	rn.mu.Unlock()

	if oldState != types.ClaimingLeader {
		rn.Emitter.StateChanged(rn.Transport.ID(), oldState, types.ClaimingLeader)
	}
	rn.Emitter.LeaderClaimStarted(rn.Transport.ID(), newTerm)

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
	rn.BroadcastToAllMembers(msg)

	go rn.waitForLeaderClaimAcks(newTerm)
}

// waitForLeaderClaimAcks waits for ACKs; if majority YES → become leader, else → Follower.
func (rn *RaftNode) waitForLeaderClaimAcks(claimTerm int64) {
	yesCount := 1 // self = YES
	totalCount := rn.Membership.GetTotalCount()
	majority := totalCount/2 + 1

	timeout := time.After(2 * rn.Config.GetHeartbeatTimeout())
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
				rn.Logger.Printf("[%s] Leader claim ack YES from %s (total YES: %d/%d, need: %d)",
					rn.Transport.ID().ShortString(), m.SenderID, yesCount, totalCount, majority)
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
		rn.Logger.Printf("[%s] *** I AM NOW THE LEADER (term %d) *** YES=%d >= majority=%d",
			rn.Transport.ID().ShortString(), claimTerm, yesCount, majority)
		go rn.sendHeartbeat()
		go func() { _ = rn.StartAutoProposeBlock(rn.Config.GetAutoProposalBlockSize()) }()
		go func() {
			rn.Emitter.StateChanged(rn.Transport.ID(), types.ClaimingLeader, types.Leader)
			rn.Emitter.BecameLeader(rn.Transport.ID(), claimTerm)
		}()
	} else {
		rn.state = types.Follower
		rn.currentTerm--
		rn.currentLeaderID = ""
		rn.Logger.Printf("[%s] Leader claim failed: YES=%d < majority=%d, reverted to term %d",
			rn.Transport.ID().ShortString(), yesCount, majority, rn.currentTerm)
		go rn.Emitter.StateChanged(rn.Transport.ID(), types.ClaimingLeader, types.Follower)
	}
}

// handleIAmNewLeader handles I AM NEW LEADER message.
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

	if expectedID != "" {
		accept := claimerID == expectedID
		if accept {
			rn.mu.Lock()
			rn.currentLeaderID = claimerID
			rn.currentTerm = data.NewTerm
			rn.lastHeartbeat = time.Now()
			rn.expectedLeaderID = ""
			rn.expectedLeaderDeadline = time.Time{}
			rn.mu.Unlock()
		}
		rn.sendLeaderClaimAck(claimerID, data.NewTerm, accept)
		return
	}

	go rn.evaluateAndAckLeaderClaim(claimerID, data)
}

// evaluateAndAckLeaderClaim evaluates the claim after waiting for current leader timeout if needed.
func (rn *RaftNode) evaluateAndAckLeaderClaim(claimerID peer.ID, data types.IAmNewLeaderClaim) {
	hp := rn.Membership.GetHighestPriorityAliveNode()
	rn.mu.RLock()
	curTerm := rn.currentTerm
	lastHB := rn.lastHeartbeat
	rn.mu.RUnlock()

	if hp != nil && hp.PeerID != claimerID {
		timeoutAt := lastHB.Add(rn.Config.GetHeartbeatTimeout())
		remaining := time.Until(timeoutAt)
		if remaining > 0 {
			rn.Logger.Printf("[%s] Waiting %v for current leader to timeout before evaluating claim from %s",
				rn.Transport.ID().ShortString(), remaining.Round(time.Millisecond), claimerID.ShortString())
			time.Sleep(remaining)
		}
		// After waiting, if the known leader has not produced a fresh heartbeat,
		// treat it as dead locally so hp recomputes correctly. Without this, a
		// follower whose own timeout hasn't fired yet keeps the old leader as hp
		// and votes NO on a valid claim from the next-priority node.
		//
		// [TC04 guard] Skip if we ARE the current leader: a Leader never receives
		// its own heartbeat, so rn.lastHeartbeat is permanently stale on it. Without
		// this guard, an old leader receiving a claim would mark itself dead, recompute
		// hp = claimer, vote YES, but stay in Leader state — then keep emitting
		// heartbeats at the new term and dragging the new leader back to Follower.
		rn.mu.RLock()
		curLeader := rn.currentLeaderID
		curLastHB := rn.lastHeartbeat
		rn.mu.RUnlock()
		if curLeader != "" && curLeader != rn.Transport.ID() &&
			time.Since(curLastHB) > rn.Config.GetHeartbeatTimeout() {
			rn.Membership.MarkDead(curLeader)
			rn.Logger.Printf("[%s] Current leader %s timed out during claim evaluation, marking dead",
				rn.Transport.ID().ShortString(), curLeader.ShortString())
		}
		hp = rn.Membership.GetHighestPriorityAliveNode()
		rn.mu.RLock()
		curTerm = rn.currentTerm
		rn.mu.RUnlock()
	}

	accept := false
	if hp != nil && hp.PeerID == claimerID && data.NewTerm >= curTerm {
		accept = true
		rn.mu.Lock()
		rn.currentLeaderID = claimerID
		rn.currentTerm = data.NewTerm
		rn.lastHeartbeat = time.Now()
		rn.mu.Unlock()
	}
	rn.sendLeaderClaimAck(claimerID, data.NewTerm, accept)
}

// sendLeaderClaimAck sends ACK (YES/NO) to claimer.
func (rn *RaftNode) sendLeaderClaimAck(claimerID peer.ID, term int64, accept bool) {
	ackData := types.LeaderClaimAckData{Accept: accept, Term: term}
	ackMsg := types.Message{
		Type:      types.MsgLeaderClaimAck,
		Term:      term,
		SenderID:  rn.Transport.ID().String(),
		Data:      ackData,
		Timestamp: time.Now(),
	}
	if err := rn.Transport.SendMessage(claimerID, ackMsg); err != nil {
		rn.Logger.Printf("[%s] Error sending leader claim ack: %v", rn.Transport.ID().ShortString(), err)
	}
	rn.Logger.Printf("[%s] Responded %v to I AM NEW LEADER from %s",
		rn.Transport.ID().ShortString(), accept, claimerID.ShortString())
	rn.Emitter.LeaderClaimAck(rn.Transport.ID(), claimerID, accept)
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

// handleLeaderClaimAck routes ack to waitForLeaderClaimAcks
func (rn *RaftNode) handleLeaderClaimAck(msg types.Message) {
	select {
	case rn.LeaderClaimAckChan <- msg:
	default:
		rn.Logger.Printf("[%s] Leader claim ack channel full, dropping ack from %s",
			rn.Transport.ID().ShortString(), msg.SenderID)
	}
}

// leaderOnSendFailure is called when leader fails to send a message to a peer.
func (rn *RaftNode) leaderOnSendFailure(peerID peer.ID) {
	if !rn.IsLeader() {
		return
	}
	rn.Membership.MarkDead(peerID)
	rn.Logger.Printf("[%s] Follower %s unreachable, marking dead and broadcasting updated membership",
		rn.Transport.ID().ShortString(), peerID.ShortString())
	rn.broadcastMembershipView()
}

// becomeLeader transitions the node to Leader state (used when there's only 1 node).
func (rn *RaftNode) becomeLeader() {
	rn.mu.Lock()
	oldState := rn.state
	rn.state = types.Leader
	rn.currentLeaderID = rn.Transport.ID()
	term := rn.currentTerm
	rn.mu.Unlock()

	rn.Logger.Printf("[%s] *** I AM NOW THE LEADER (term %d) ***",
		rn.Transport.ID().ShortString(), term)

	if oldState != types.Leader {
		rn.Emitter.StateChanged(rn.Transport.ID(), oldState, types.Leader)
		rn.Emitter.BecameLeader(rn.Transport.ID(), term)
	}

	go rn.sendHeartbeat()
	go func() { _ = rn.StartAutoProposeBlock(rn.Config.GetAutoProposalBlockSize()) }()
}
