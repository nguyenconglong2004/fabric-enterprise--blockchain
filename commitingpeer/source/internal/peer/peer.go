package peer

import (
	"context"
	"encoding/hex"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"commiting-peer/internal/deliver"
	"commiting-peer/internal/storage"
	"commiting-peer/internal/types"
	"commiting-peer/internal/validation"
)

// Stats is a snapshot of the peer's runtime state.
type Stats struct {
	OrdeerAddr    string
	BlockCount    int64
	LastBlockHash string // hex, empty if no block committed yet
	LastBlockTime time.Time
	LastBlockTxs  int
}

// CommittingPeer wires together all subsystems:
//
//	Orderer  →  deliver.Client  →  blockChan
//	blockChan  →  validation.Engine  →  storage.BlockStorage + storage.WorldState
type CommittingPeer struct {
	deliverClient *deliver.Client
	validator     *validation.Engine
	blockStore    *storage.BlockStorage
	worldState    *storage.WorldState

	// blockChan is the internal pipeline channel between the deliver goroutine
	// (producer) and the commit loop (consumer).
	blockChan chan types.Block

	// runtime stats (thread-safe)
	blockCount    int64 // atomic
	mu            sync.RWMutex
	lastBlockHash []byte
	lastBlockTime time.Time
	lastBlockTxs  int
	ordererAddr   string
}

// New creates a CommittingPeer. Call Start to begin streaming.
func New(
	deliverClient *deliver.Client,
	validator *validation.Engine,
	blockStore *storage.BlockStorage,
	worldState *storage.WorldState,
) *CommittingPeer {
	return &CommittingPeer{
		deliverClient: deliverClient,
		validator:     validator,
		blockStore:    blockStore,
		worldState:    worldState,
		blockChan:     make(chan types.Block, 64),
	}
}

// Start subscribes to the ordering service at ordererAddr, beginning from
// fromIndex (1-based, inclusive), and starts the background commit loop.
// Returns immediately after launching the goroutines.
func (p *CommittingPeer) Start(ctx context.Context, ordererAddr string, fromIndex int64) error {
	p.mu.Lock()
	p.ordererAddr = ordererAddr
	p.mu.Unlock()

	if err := p.deliverClient.Subscribe(ctx, ordererAddr, fromIndex, p.blockChan); err != nil {
		return err
	}
	go p.commitLoop(ctx)
	return nil
}

// commitLoop reads blocks from blockChan, validates them, then persists to
// both BlockStorage (file) and WorldState (LevelDB).
func (p *CommittingPeer) commitLoop(ctx context.Context) {
	for {
		select {
		case block := <-p.blockChan:
			p.handleBlock(block)
		case <-ctx.Done():
			log.Println("[peer] commit loop stopped")
			return
		}
	}
}

func (p *CommittingPeer) handleBlock(block types.Block) {
	hashHex := hex.EncodeToString(block.Hash)

	if err := p.validator.ValidateBlock(block); err != nil {
		log.Printf("[peer] block rejected hash=%s: %v", hashHex, err)
		return
	}

	if err := p.blockStore.AppendBlock(block); err != nil {
		log.Printf("[peer] failed to persist block hash=%s: %v", hashHex, err)
		return
	}

	if err := p.worldState.ApplyBlock(block); err != nil {
		log.Printf("[peer] failed to apply block to world state hash=%s: %v", hashHex, err)
		return
	}

	// Update stats atomically.
	atomic.AddInt64(&p.blockCount, 1)
	p.mu.Lock()
	p.lastBlockHash = block.Hash
	p.lastBlockTime = time.Unix(block.Timestamp, 0)
	p.lastBlockTxs = len(block.Transactions)
	p.mu.Unlock()

	log.Printf("[peer] committed block hash=%s txs=%d", hashHex, len(block.Transactions))
}

// GetStats returns a snapshot of the current peer runtime state.
func (p *CommittingPeer) GetStats() Stats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	hashHex := ""
	if len(p.lastBlockHash) > 0 {
		hashHex = hex.EncodeToString(p.lastBlockHash)
	}
	return Stats{
		OrdeerAddr:    p.ordererAddr,
		BlockCount:    atomic.LoadInt64(&p.blockCount),
		LastBlockHash: hashHex,
		LastBlockTime: p.lastBlockTime,
		LastBlockTxs:  p.lastBlockTxs,
	}
}

// Stop closes all underlying resources.
func (p *CommittingPeer) Stop() {
	p.deliverClient.Close()
	if err := p.blockStore.Close(); err != nil {
		log.Printf("[peer] close block store: %v", err)
	}
	if err := p.worldState.Close(); err != nil {
		log.Printf("[peer] close world state: %v", err)
	}
}
