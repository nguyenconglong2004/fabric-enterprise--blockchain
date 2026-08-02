package peer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/network"

	"commiting-peer/internal/deliver"
	"commiting-peer/internal/discovery"
	"commiting-peer/internal/metrics"
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
//	blockChan  →  validation  →  BlockStorage (file) + WorldState (LevelDB)
//	Postgres mirror runs async (explorer); chain commit completes before PG write.
type CommittingPeer struct {
	deliverClient *deliver.Client
	validator     *validation.Engine
	blockStore    *storage.BlockStorage
	worldState    *storage.WorldState
	db            *storage.PostgresDB
	ledgerMirror  chan ledgerMirrorJob

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
	p := &CommittingPeer{
		deliverClient: deliverClient,
		validator:     validator,
		blockStore:    blockStore,
		worldState:    worldState,
		db:            db,
		blockChan:     make(chan types.Block, 64),
	}
	p.initLedgerMirror()
	return p
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
	p.startLedgerMirror(ctx)

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

	// 1-based block height for DB + MVCC version strings.
	blockNumber := atomic.AddInt64(&p.blockCount, 1)
	results, err := p.worldState.ApplyBlock(block, blockNumber)
	if err != nil {
		log.Printf("[peer] failed to apply block to world state hash=%s: %v", hashHex, err)
		return
	}
	for _, r := range results {
		if r.Code == storage.TxInvalidMVCC {
			log.Printf("[peer] tx %s INVALID_MVCC: %s", r.Txid, r.Reason)
		}
	}

	p.mu.Lock()
	p.lastBlockHash = block.Hash
	p.lastBlockTime = time.Unix(block.Timestamp, 0)
	p.lastBlockTxs = len(block.Transactions)
	p.mu.Unlock()

	committedAt := time.Now().UTC()
	validN := 0
	for _, r := range results {
		if r.Code == storage.TxValid {
			validN++
		}
	}
	log.Printf("[peer] committed block hash=%s txs=%d valid=%d", hashHex, len(block.Transactions), validN)

	txids := make([]string, 0, len(block.Transactions))
	for _, tx := range block.Transactions {
		if tx.Txid != "" {
			txids = append(txids, tx.Txid)
		}
	}
	metrics.DefaultRecorder.RecordBlock(hashHex, txids, committedAt)

	// Explorer mirror only — does not affect commit success.
	p.enqueueLedgerMirror(block, hashHex, blockNumber, committedAt)
}

// saveBlockToDatabase persists block + txs in one DB transaction (batch insert).
func (p *CommittingPeer) saveBlockToDatabase(block types.Block, hashHex string, blockNumber int64, committedAt time.Time) {
	rows := make([]storage.LedgerTxRow, 0, len(block.Transactions))
	for i, tx := range block.Transactions {
		txData, err := ledgerTransactionRecord(tx)
		if err != nil {
			log.Printf("[peer] encode tx %s: %v", tx.Txid, err)
			continue
		}
		raw, err := json.Marshal(txData)
		if err != nil {
			log.Printf("[peer] marshal tx %s: %v", tx.Txid, err)
			continue
		}
		rows = append(rows, storage.LedgerTxRow{Txid: tx.Txid, TxIndex: i, TxData: raw})
	}
	if err := p.db.SaveBlockWithTransactions(hashHex, blockNumber, block, rows, committedAt); err != nil {
		log.Printf("[peer] postgres mirror failed hash=%s: %v", hashHex, err)
	}
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
