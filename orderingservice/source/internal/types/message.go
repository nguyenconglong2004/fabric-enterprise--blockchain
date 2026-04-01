package types

import "time"

// MessageType represents different message types in the protocol
type MessageType int

const (
	MsgHeartbeat MessageType = iota
	MsgHeartbeatResponse
	MsgIAmNewLeader
	MsgLeaderClaimAck
	MsgMembershipUpdate
	MsgMembershipAck
	MsgMembershipRequest
	MsgMembershipResponse
	MsgTxRequest
	MsgTxResponse
	MsgBlockProposal
	MsgBlockProposalAck
	MsgBlockCommit
)

func (mt MessageType) String() string {
	switch mt {
	case MsgHeartbeat:
		return "Heartbeat"
	case MsgHeartbeatResponse:
		return "HeartbeatResponse"
	case MsgIAmNewLeader:
		return "IAmNewLeader"
	case MsgLeaderClaimAck:
		return "LeaderClaimAck"
	case MsgMembershipUpdate:
		return "MembershipUpdate"
	case MsgMembershipAck:
		return "MembershipAck"
	case MsgMembershipRequest:
		return "MembershipRequest"
	case MsgMembershipResponse:
		return "MembershipResponse"
	case MsgTxRequest:
		return "TxRequest"
	case MsgTxResponse:
		return "TxResponse"
	case MsgBlockProposal:
		return "BlockProposal"
	case MsgBlockProposalAck:
		return "BlockProposalAck"
	case MsgBlockCommit:
		return "BlockCommit"
	default:
		return "Unknown"
	}
}

// Message represents a protocol message
type Message struct {
	Type      MessageType
	Term      int64
	SenderID  string
	Data      interface{}
	Timestamp time.Time
}

// IAmNewLeaderClaim is sent by the highest priority node to claim leadership
type IAmNewLeaderClaim struct {
	NewLeaderID string
	NewTerm     int64
	Priority    int
}

// LeaderClaimAckData is the response YES/NO to I AM NEW LEADER
type LeaderClaimAckData struct {
	Accept bool // true = YES (agree), false = NO (disagree)
	Term   int64
}

// HeartbeatResponse is sent by a follower back to a stale leader.
// It informs the stale leader of the current term, current leader, and membership state
// so it can step down and resync without waiting for a new heartbeat.
type HeartbeatResponse struct {
	CurrentTerm     int64
	CurrentLeaderID string
	MembershipData  map[string]interface{}
}

// MembershipProposal is sent when a new member wants to join
type MembershipProposal struct {
	PeerID   string
	JoinTime time.Time
	Version  int64
}
