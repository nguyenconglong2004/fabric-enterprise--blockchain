package types

import (
	"sync"
	"time"
)

const LogTypeBlockProposing = "BLOCK_PROPOSING"

// Block represents a committed block containing multiple transactions
type Block struct {
	BlockID      string
	Transactions []TransactionWrapper
	Timestamp    time.Time
}

// LogEntry is a Raft log entry (not yet committed)
type LogEntry struct {
	Index        int64 // position in the log, starts from 1
	PrevLogIndex int64 // index of the previous entry (0 if first entry)
	Term         int64
	Type         string // e.g. LogTypeBlockProposing
	Block        Block
}

// BlockProposal is sent by leader to propose a new log entry
type BlockProposal struct {
	Entry     LogEntry
	Timestamp time.Time
}

// BlockProposalAck is sent by followers to acknowledge a block proposal
type BlockProposalAck struct {
	BlockID   string
	LogIndex  int64
	Accepted  bool
	Reason    string
	Timestamp time.Time
}

// BlockCommit is sent by leader to commit a block
type BlockCommit struct {
	BlockID   string
	LogIndex  int64
	Timestamp time.Time
}

// RaftLog stores uncommitted log entries
type RaftLog struct {
	mu      sync.RWMutex
	Entries []LogEntry
}

func NewRaftLog() *RaftLog {
	return &RaftLog{
		Entries: make([]LogEntry, 0),
	}
}

func (rl *RaftLog) AppendEntry(entry LogEntry) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.Entries = append(rl.Entries, entry)
}

// GetLastIndex returns the index of the last entry, or 0 if empty
func (rl *RaftLog) GetLastIndex() int64 {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	if len(rl.Entries) == 0 {
		return 0
	}
	return rl.Entries[len(rl.Entries)-1].Index
}

// FindEntryByIndex returns the log entry at the given index, or nil
func (rl *RaftLog) FindEntryByIndex(index int64) *LogEntry {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	for i := range rl.Entries {
		if rl.Entries[i].Index == index {
			return &rl.Entries[i]
		}
	}
	return nil
}

// RemoveFrom removes all entries with Index >= fromIndex (log truncation)
func (rl *RaftLog) RemoveFrom(fromIndex int64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for i, e := range rl.Entries {
		if e.Index >= fromIndex {
			rl.Entries = rl.Entries[:i]
			return
		}
	}
}

// GetEntries returns a copy of all entries
func (rl *RaftLog) GetEntries() []LogEntry {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return append([]LogEntry{}, rl.Entries...)
}

// OrderingBlock stores committed blocks
type OrderingBlock struct {
	mu     sync.RWMutex
	Blocks []Block
}

func NewOrderingBlock() *OrderingBlock {
	return &OrderingBlock{
		Blocks: make([]Block, 0),
	}
}

func (ob *OrderingBlock) AppendBlock(block Block) {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	ob.Blocks = append(ob.Blocks, block)
}

func (ob *OrderingBlock) GetBlocks() []Block {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return append([]Block{}, ob.Blocks...)
}

// GetLastIndex returns the total number of committed blocks (1-based count)
func (ob *OrderingBlock) GetLastIndex() int64 {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return int64(len(ob.Blocks))
}
