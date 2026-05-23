package peer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/network"

	"commiting-peer/internal/deliver"
	"commiting-peer/internal/discovery"
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
//	blockChan  →  validation  →  BlockStorage (file) + WorldState (LevelDB) + PostgresDB (ledger + txs only)
type CommittingPeer struct {
	deliverClient *deliver.Client
	validator     *validation.Engine
	blockStore    *storage.BlockStorage
	worldState    *storage.WorldState
	db            *storage.PostgresDB

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

	// OrderDiscovery resolves alive orderers after leader failover (optional).
	OrderDiscovery *discovery.Client
}

// New creates a CommittingPeer. Call Start to begin streaming.
func New(
	deliverClient *deliver.Client,
	validator *validation.Engine,
	blockStore *storage.BlockStorage,
	worldState *storage.WorldState,
	db *storage.PostgresDB,
) *CommittingPeer {
	return &CommittingPeer{
		deliverClient: deliverClient,
		validator:     validator,
		blockStore:    blockStore,
		worldState:    worldState,
		db:            db,
		blockChan:     make(chan types.Block, 64),
	}
}

// Start subscribes to the ordering service at ordererAddr, beginning from
// fromIndex (1-based, inclusive), and starts the background commit loop.
// When OrderDiscovery is set, deliver reconnects across alive orderers after disconnect.
// Returns immediately after launching the goroutines.
func (p *CommittingPeer) Start(ctx context.Context, ordererAddr string, fromIndex int64) error {
	p.mu.Lock()
	p.ordererAddr = ordererAddr
	// Align counters with chain file so new blocks get correct heights / stats after restart.
	if p.blockStore != nil {
		if n := p.blockStore.CommittedBlockCount(); n > 0 {
			atomic.StoreInt64(&p.blockCount, n)
			if tip := p.blockStore.CommittedTipHash(); len(tip) > 0 {
				p.lastBlockHash = append([]byte(nil), tip...)
			}
		}
	}
	p.mu.Unlock()

	go p.commitLoop(ctx)

	if p.OrderDiscovery != nil {
		go p.deliverReconnectLoop(ctx, ordererAddr, fromIndex)
		return nil
	}

	if _, err := p.deliverClient.Subscribe(ctx, ordererAddr, fromIndex, p.blockChan); err != nil {
		return err
	}
	return nil
}

// deliverFromIndex returns the next block index to request (1-based).
func (p *CommittingPeer) deliverFromIndex(fallback int64) int64 {
	if p.blockStore == nil {
		return fallback
	}
	n := p.blockStore.CommittedBlockCount()
	if n > 0 {
		return n + 1
	}
	return fallback
}

// deliverReconnectLoop keeps a deliver subscription alive, picking alive orderers via discovery.
func (p *CommittingPeer) deliverReconnectLoop(ctx context.Context, fallbackOrderer string, initialFrom int64) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			log.Println("[peer] deliver reconnect loop stopped")
			return
		default:
		}

		fromIndex := p.deliverFromIndex(initialFrom)
		orderer, err := p.pickOrdererForDeliver(ctx, fallbackOrderer, attempt)
		if err != nil {
			log.Printf("[peer] deliver: no orderer available: %v (retry in %s)", err, backoff)
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = minDuration(backoff*2, maxBackoff)
			attempt++
			continue
		}

		p.mu.Lock()
		p.ordererAddr = orderer
		p.mu.Unlock()

		done, err := p.deliverClient.Subscribe(ctx, orderer, fromIndex, p.blockChan)
		if err != nil {
			log.Printf("[peer] deliver: subscribe to %s failed: %v (retry in %s)", shortOrderer(orderer), err, backoff)
			if p.OrderDiscovery != nil {
				p.OrderDiscovery.Invalidate()
			}
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = minDuration(backoff*2, maxBackoff)
			attempt++
			continue
		}

		log.Printf("[peer] deliver: connected to %s from_index=%d", shortOrderer(orderer), fromIndex)
		backoff = time.Second

		select {
		case <-ctx.Done():
			return
		case <-done:
			log.Printf("[peer] deliver: stream ended from %s, reconnecting...", shortOrderer(orderer))
			if p.OrderDiscovery != nil {
				p.OrderDiscovery.Invalidate()
			}
			attempt++
			if !sleepCtx(ctx, time.Second) {
				return
			}
		}
	}
}

func (p *CommittingPeer) pickOrdererForDeliver(ctx context.Context, fallback string, attempt int) (string, error) {
	if p.OrderDiscovery == nil {
		if fallback == "" {
			return "", fmt.Errorf("no fallback orderer address")
		}
		return fallback, nil
	}

	mv, err := p.OrderDiscovery.Snapshot(ctx)
	if err != nil {
		return "", err
	}

	addrs, err := discovery.PickAllAliveOrdererAddrs(mv)
	if err != nil {
		// Last resort: configured fallback if still dialable.
		if fallback != "" {
			return fallback, nil
		}
		return "", err
	}
	if len(addrs) == 0 && fallback != "" {
		return fallback, nil
	}
	return addrs[attempt%len(addrs)], nil
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func shortOrderer(addr string) string {
	if len(addr) <= 56 {
		return addr
	}
	return addr[:28] + "..." + addr[len(addr)-20:]
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

	if err := p.validator.ValidateBlock(block, p.blockStore.CommittedTipHash()); err != nil {
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

	// 1-based block height for DB (stable for this block even if async DB write runs later).
	blockNumber := atomic.AddInt64(&p.blockCount, 1)
	p.mu.Lock()
	p.lastBlockHash = block.Hash
	p.lastBlockTime = time.Unix(block.Timestamp, 0)
	p.lastBlockTxs = len(block.Transactions)
	p.mu.Unlock()

	log.Printf("[peer] committed block hash=%s txs=%d", hashHex, len(block.Transactions))

	// Mirror block + txs to PostgreSQL (explorer); world state stays in LevelDB only.
	if p.db != nil {
		go p.saveBlockToDatabase(block, hashHex, blockNumber)
	}
}

// saveBlockToDatabase persists the committed block and its transactions to PostgreSQL.
// Full-flow timing (submit → SoT) uses ledger_committed_at on each ledger_transactions row.
func (p *CommittingPeer) saveBlockToDatabase(block types.Block, hashHex string, blockNumber int64) {
	blockLedgerAt := time.Now().UTC()
	blockID, err := p.db.SaveBlockToLedger(
		hashHex, blockNumber, block, len(block.Transactions), blockLedgerAt,
	)
	if err != nil {
		log.Printf("[peer] failed to save block to database hash=%s: %v", hashHex, err)
		return
	}

	var (
		savedTxs      int
		minSubmitMs   int64 = 0
		maxLedgerAt         = blockLedgerAt
		hasSubmit           bool
		sumE2EMs      int64
	)

	// Save each transaction: full wire-format tx (vin, vout, hex payload, …) + payload_decoded when JSON.
	for i, tx := range block.Transactions {
		txData, err := ledgerTransactionRecord(tx)
		if err != nil {
			log.Printf("[peer] failed to encode transaction for ledger txid=%s: %v", tx.Txid, err)
			continue
		}

		txLedgerAt := time.Now().UTC()
		if err := p.db.SaveTransactionToLedger(
			blockID, tx.Txid, i, txData, tx.SubmittedAtMs, txLedgerAt,
		); err != nil {
			log.Printf("[peer] failed to save transaction to database txid=%s: %v", tx.Txid, err)
			continue
		}
		savedTxs++

		if tx.SubmittedAtMs > 0 {
			if !hasSubmit || tx.SubmittedAtMs < minSubmitMs {
				minSubmitMs = tx.SubmittedAtMs
				hasSubmit = true
			}
			e2eMs := txLedgerAt.Sub(time.UnixMilli(tx.SubmittedAtMs)).Milliseconds()
			if e2eMs < 0 {
				e2eMs = 0
			}
			sumE2EMs += e2eMs
			if txLedgerAt.After(maxLedgerAt) {
				maxLedgerAt = txLedgerAt
			}
			if e2eLogTx() {
				log.Printf(
					"[e2e] ledger SoT txid=%s submitted_at_ms=%d ledger_committed_at=%s e2e_ms=%d (commit_peer.ledger_transactions)",
					tx.Txid, tx.SubmittedAtMs, txLedgerAt.Format(time.RFC3339Nano), e2eMs,
				)
			}
		} else if e2eLogTx() {
			log.Printf("[e2e] ledger SoT txid=%s ledger_committed_at=%s (no submitted_at_ms — E2E N/A)",
				tx.Txid, txLedgerAt.Format(time.RFC3339Nano))
		}
	}

	var blockE2EMs int64
	if hasSubmit && savedTxs > 0 {
		blockE2EMs = maxLedgerAt.Sub(time.UnixMilli(minSubmitMs)).Milliseconds()
		if blockE2EMs < 0 {
			blockE2EMs = 0
		}
	}
	avgE2E := int64(0)
	if savedTxs > 0 && hasSubmit {
		avgE2E = sumE2EMs / int64(savedTxs)
	}

	log.Printf(
		"[e2e] ledger SoT block closed block_number=%d block_hash=%s txs=%d block_e2e_ms=%d avg_tx_e2e_ms=%d block_ledger_at=%s",
		blockNumber, hashHex[:min(16, len(hashHex))], savedTxs, blockE2EMs, avgE2E, blockLedgerAt.Format(time.RFC3339Nano),
	)
	log.Printf("[peer] successfully saved block hash=%s with %d transactions to database", hashHex, savedTxs)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func e2eLogTx() bool {
	return strings.TrimSpace(os.Getenv("E2E_LOG_TX")) == "1"
}

// ledgerTransactionRecord matches order/core JSON (hex payload, locktime, vin, vout, contract fields).
// If payload bytes are JSON (e.g. example_asset: id, color, action), payload_decoded is added for explorers.
func ledgerTransactionRecord(tx types.Transaction) (map[string]interface{}, error) {
	wire, err := json.Marshal(tx)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(wire, &out); err != nil {
		return nil, err
	}
	if len(tx.Payload) > 0 {
		var dec interface{}
		if err := json.Unmarshal(tx.Payload, &dec); err == nil {
			out["payload_decoded"] = dec
		}
	}
	return out, nil
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

// HandleSyncStream handles an incoming sync stream from an ordering service client.
// It reads a SyncRequest containing a wallet address, then responds with all UTXOs
// from the world state whose ScriptPubKey.Addresses contains that address.
func (p *CommittingPeer) HandleSyncStream(s network.Stream) {
	defer s.Close()

	var req types.SyncRequest
	if err := json.NewDecoder(s).Decode(&req); err != nil {
		log.Printf("[peer] sync: failed to decode request: %v", err)
		return
	}

	allUTXOs, err := p.worldState.AllUTXOs()
	if err != nil {
		log.Printf("[peer] sync: failed to read world state: %v", err)
		return
	}

	var matched []types.SyncUTXO
	for _, entry := range allUTXOs {
		for _, addr := range entry.Out.ScriptPubKey.Addresses {
			if addr == req.Address {
				matched = append(matched, types.SyncUTXO{
					Txid:    entry.Txid,
					VoutIdx: entry.Index,
					Out:     entry.Out,
				})
				break
			}
		}
	}

	resp := types.SyncResponse{UTXOs: matched}
	if err := json.NewEncoder(s).Encode(resp); err != nil {
		log.Printf("[peer] sync: failed to encode response: %v", err)
		return
	}

	log.Printf("[peer] sync: address=%s matched=%d UTXOs", req.Address, len(matched))
}

// RegisterSyncHandler registers the HandleSyncStream method on the deliver client
// so incoming sync connections from ordering service clients are handled.
func (p *CommittingPeer) RegisterSyncHandler() {
	p.deliverClient.SetStreamHandler(deliver.SyncProtocolID, p.HandleSyncStream)
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
