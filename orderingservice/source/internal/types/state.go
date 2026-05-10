package types

// NodeState represents the current state of a node
type NodeState int

const (
	Follower NodeState = iota
	Leader
	ClaimingLeader // đang gửi I AM NEW LEADER và chờ đủ majority YES
	Syncing        // đang đồng bộ blocks/log từ các peer khác (first-join hoặc rejoin)
)

func (s NodeState) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Leader:
		return "Leader"
	case ClaimingLeader:
		return "ClaimingLeader"
	case Syncing:
		return "Syncing"
	default:
		return "Unknown"
	}
}
