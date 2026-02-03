package types

// NodeState represents the current state of a node
type NodeState int

const (
	Follower NodeState = iota
	Leader
	ClaimingLeader // đang gửi I AM NEW LEADER và chờ đủ majority YES
)

func (s NodeState) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Leader:
		return "Leader"
	case ClaimingLeader:
		return "ClaimingLeader"
	default:
		return "Unknown"
	}
}
