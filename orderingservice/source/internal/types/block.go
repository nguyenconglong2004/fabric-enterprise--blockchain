package types

import "time"

// Block represents a block containing multiple transactions (orders)
type Block struct {
	BlockID    string    // Unique block ID
	Orders     []Order   // List of orders in this block
	Timestamp  time.Time // When block was created
	ProposerID string    // ID of the node that proposed this block
	Term       int64     // Term when block was proposed
}

// BlockProposal is sent by leader to propose a new block
type BlockProposal struct {
	Block     Block
	Timestamp time.Time
}

// BlockProposalAck is sent by followers to acknowledge block proposal
type BlockProposalAck struct {
	BlockID   string
	Accepted  bool   // Whether the proposal is accepted
	Reason    string // Reason if rejected
	Timestamp time.Time
}

// BlockCommit is sent by leader to commit a block
type BlockCommit struct {
	BlockID   string
	OrderIDs  []string // List of order IDs in this block
	Timestamp time.Time
}
