package raft

import (
	"raft-order-service/internal/types"
)

// processMessages processes incoming messages
func (rn *RaftNode) processMessages() {
	for {
		select {
		case <-rn.stopChan:
			return
		case msg := <-rn.MessageChan:
			rn.handleMessage(msg)
		}
	}
}

// handleMessage handles different types of messages
func (rn *RaftNode) handleMessage(msg types.Message) {
	// During sync, defer block proposal/commit to avoid duplicate work.
	if rn.IsSyncing() {
		switch msg.Type {
		case types.MsgBlockProposal, types.MsgBlockCommit:
			rn.Logger.Printf("[%s] sync: deferring %s during sync", rn.Transport.ID().ShortString(), msg.Type)
			return
		}
	}

	switch msg.Type {
	case types.MsgHeartbeat:
		rn.handleHeartbeat(msg)
	case types.MsgHeartbeatResponse:
		rn.handleHeartbeatResponse(msg)
	case types.MsgIAmNewLeader:
		rn.handleIAmNewLeader(msg)
	case types.MsgLeaderClaimAck:
		rn.handleLeaderClaimAck(msg)
	case types.MsgMembershipUpdate:
		rn.handleMembershipUpdate(msg)
	case types.MsgMembershipAck:
		rn.handleMembershipAck(msg)
	case types.MsgMembershipRequest:
		rn.handleMembershipRequest(msg)
	case types.MsgMembershipResponse:
		select {
		case rn.MembershipResponseChan <- msg:
		default:
			rn.Logger.Printf("[%s] membership response channel full, dropping", rn.Transport.ID().ShortString())
		}
	case types.MsgTxRequest:
		rn.HandleTxRequest(msg)
	case types.MsgTxResponse:
		rn.HandleTxResponse(msg)
	case types.MsgBlockProposal:
		rn.HandleBlockProposal(msg)
	case types.MsgBlockProposalAck:
		rn.HandleBlockProposalAck(msg)
	case types.MsgBlockCommit:
		rn.HandleBlockCommit(msg)
	case types.MsgSyncStatusRequest:
		rn.handleSyncStatusRequest(msg)
	case types.MsgSyncStatusResponse:
		select {
		case rn.SyncStatusChan <- msg:
		default:
			rn.Logger.Printf("[%s] sync: status response channel full, dropping", rn.Transport.ID().ShortString())
		}
	default:
		rn.Logger.Printf("[%s] Unknown message type: %v", rn.Transport.ID().ShortString(), msg.Type)
	}
}
