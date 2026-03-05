package raft

import (
	"log"

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
	switch msg.Type {
	case types.MsgHeartbeat:
		rn.handleHeartbeat(msg)
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
	default:
		log.Printf("[%s] Unknown message type: %v", rn.Transport.ID().ShortString(), msg.Type)
	}
}
