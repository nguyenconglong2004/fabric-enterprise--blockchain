package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"runtime"
	"sync"
	"time"
)

const LogTypeBlockProposing = "BLOCK_PROPOSING"

// Block is a cryptographically linked block of transactions.
type Block struct {
	Timestamp    int64         `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
	PrevHash     []byte        `json:"prev_hash"`
	Hash         []byte        `json:"hash"`
	Nonce        int           `json:"nonce"`
	MerkleRoot   []byte        `json:"merkle_root"`
	Size         int           `json:"size"`
}

// BlockID returns the hex-encoded block hash, used as a unique string identifier.
func (b *Block) BlockID() string {
	return hex.EncodeToString(b.Hash)
}

// NewBlock creates a new block from the given transactions and previous hash.
func NewBlock(txs []Transaction, prevHash []byte) *Block {
	b := &Block{
		Timestamp:    time.Now().Unix(),
		Transactions: txs,
		PrevHash:     prevHash,
		Nonce:        0,
	}
	// Compute total size
	for _, tx := range txs {
		b.Size += tx.Size()
	}
	b.MerkleRoot = ComputeMerkleRoot(txs)
	b.Hash = b.BlockHash()
	return b
}

// NewGenesisBlock creates the genesis block with an empty transaction list.
func NewGenesisBlock() *Block {
	return NewBlock([]Transaction{}, []byte{})
}

// SerializeHeader serializes the block header in Bitcoin-like format.
func (b *Block) SerializeHeader() []byte {
	buf := new(bytes.Buffer)

	binary.Write(buf, binary.LittleEndian, uint32(1)) // version

	prev := make([]byte, 32)
	copy(prev[32-len(b.PrevHash):], b.PrevHash)
	buf.Write(prev)

	mr := make([]byte, 32)
	copy(mr[32-len(b.MerkleRoot):], b.MerkleRoot)
	buf.Write(mr)

	binary.Write(buf, binary.LittleEndian, uint32(b.Timestamp))
	binary.Write(buf, binary.LittleEndian, uint32(0))         // bits (difficulty)
	binary.Write(buf, binary.LittleEndian, uint32(b.Nonce))

	return buf.Bytes()
}

// BlockHash returns the double-SHA256 of the block header.
func (b *Block) BlockHash() []byte {
	h := b.SerializeHeader()
	h1 := sha256.Sum256(h)
	h2 := sha256.Sum256(h1[:])
	return h2[:]
}

// ComputeMerkleRoot computes the Merkle root of the transaction list.
func ComputeMerkleRoot(txs []Transaction) []byte {
	n := len(txs)
	if n == 0 {
		return make([]byte, 32)
	}
	if n == 1 {
		h1 := sha256.Sum256([]byte(txs[0].Txid))
		h2 := sha256.Sum256(h1[:])
		return h2[:]
	}

	hashes := make([][]byte, n)
	for i := range txs {
		hashes[i] = []byte(txs[i].Txid)
	}

	// Parallel first level for large blocks
	if n > 1000 {
		numWorkers := runtime.NumCPU()
		var wg sync.WaitGroup
		chunkSize := (n + numWorkers - 1) / numWorkers
		nextLevel := make([][]byte, (n+1)/2)

		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				start := workerID * chunkSize * 2
				end := start + chunkSize*2
				if end > n {
					end = n
				}
				buf := make([]byte, 64)
				for i := start; i < end; i += 2 {
					if i >= n {
						break
					}
					copy(buf[:32], hashes[i])
					if i+1 < n {
						copy(buf[32:], hashes[i+1])
					} else {
						copy(buf[32:], hashes[i])
					}
					h1 := sha256.Sum256(buf)
					h2 := sha256.Sum256(h1[:])
					hash := make([]byte, 32)
					copy(hash, h2[:])
					nextLevel[i/2] = hash
				}
			}(w)
		}
		wg.Wait()
		hashes = nextLevel
	}

	// Sequential remaining levels
	buf := make([]byte, 64)
	for len(hashes) > 1 {
		nextLen := (len(hashes) + 1) / 2
		nextLevel := make([][]byte, 0, nextLen)
		for i := 0; i < len(hashes); i += 2 {
			copy(buf[:32], hashes[i])
			if i+1 < len(hashes) {
				copy(buf[32:], hashes[i+1])
			} else {
				copy(buf[32:], hashes[i])
			}
			h1 := sha256.Sum256(buf)
			h2 := sha256.Sum256(h1[:])
			hash := make([]byte, 32)
			copy(hash, h2[:])
			nextLevel = append(nextLevel, hash)
		}
		hashes = nextLevel
	}

	return hashes[0]
}

// LogEntry is a Raft log entry (not yet committed).
type LogEntry struct {
	Index        int64  `json:"index"`
	PrevLogIndex int64  `json:"prev_log_index"`
	Term         int64  `json:"term"`
	Type         string `json:"type"`
	Block        Block  `json:"block"`
}

// BlockProposal is sent by leader to propose a new log entry.
type BlockProposal struct {
	Entry     LogEntry  `json:"entry"`
	Timestamp time.Time `json:"timestamp"`
}

// BlockProposalAck is sent by followers to acknowledge a block proposal.
type BlockProposalAck struct {
	BlockID   string    `json:"block_id"`
	LogIndex  int64     `json:"log_index"`
	Accepted  bool      `json:"accepted"`
	Reason    string    `json:"reason"`
	Timestamp time.Time `json:"timestamp"`
}

// BlockCommit is sent by leader to commit a block.
type BlockCommit struct {
	BlockID   string    `json:"block_id"`
	LogIndex  int64     `json:"log_index"`
	Timestamp time.Time `json:"timestamp"`
}

// RaftLog stores uncommitted log entries.
type RaftLog struct {
	mu      sync.RWMutex
	Entries []LogEntry
}

func NewRaftLog() *RaftLog {
	return &RaftLog{Entries: make([]LogEntry, 0)}
}

func (rl *RaftLog) AppendEntry(entry LogEntry) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.Entries = append(rl.Entries, entry)
}

// GetLastIndex returns the index of the last entry, or 0 if empty.
func (rl *RaftLog) GetLastIndex() int64 {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	if len(rl.Entries) == 0 {
		return 0
	}
	return rl.Entries[len(rl.Entries)-1].Index
}

// FindEntryByIndex returns the log entry at the given index, or nil.
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

// RemoveFrom removes all entries with Index >= fromIndex (log truncation).
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

// GetEntries returns a copy of all entries.
func (rl *RaftLog) GetEntries() []LogEntry {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return append([]LogEntry{}, rl.Entries...)
}

// OrderingBlock stores committed blocks.
type OrderingBlock struct {
	mu     sync.RWMutex
	Blocks []Block
}

func NewOrderingBlock() *OrderingBlock {
	return &OrderingBlock{Blocks: make([]Block, 0)}
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

// GetLastIndex returns the total number of committed blocks (1-based count).
func (ob *OrderingBlock) GetLastIndex() int64 {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return int64(len(ob.Blocks))
}
