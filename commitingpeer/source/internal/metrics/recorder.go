package metrics

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// txCommit records when a transaction was committed on this peer (ground truth).
type txCommit struct {
	at time.Time
}

// blockCommit records one committed block for throughput queries.
type blockCommit struct {
	at    time.Time
	hash  string
	txids []string
}

// Recorder stores commit timestamps in memory for benchmark APIs (no Postgres mirror lag).
type Recorder struct {
	mu      sync.RWMutex
	txs     map[string]txCommit
	blocks  []blockCommit
	enabled bool
	retain  time.Duration
}

// DefaultRecorder is the process-wide commit metrics store.
var DefaultRecorder = NewRecorder()

func NewRecorder() *Recorder {
	enabled := os.Getenv("COMMIT_PEER_RECORD_METRICS") != "0"
	retain := 2 * time.Hour
	if s := strings.TrimSpace(os.Getenv("COMMIT_PEER_METRICS_RETENTION")); s != "" {
		if sec, err := strconv.Atoi(s); err == nil && sec > 0 {
			retain = time.Duration(sec) * time.Second
		}
	}
	return &Recorder{
		txs:     make(map[string]txCommit),
		blocks:  make([]blockCommit, 0, 256),
		enabled: enabled,
		retain:  retain,
	}
}

func (r *Recorder) Enabled() bool {
	return r != nil && r.enabled
}

// RecordBlock stores commit times for all transactions in a block.
func (r *Recorder) RecordBlock(hashHex string, txids []string, committedAt time.Time) {
	if r == nil || !r.enabled || committedAt.IsZero() {
		return
	}
	at := committedAt.UTC()
	ids := append([]string(nil), txids...)

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, id := range ids {
		if id == "" {
			continue
		}
		r.txs[id] = txCommit{at: at}
	}
	r.blocks = append(r.blocks, blockCommit{at: at, hash: hashHex, txids: ids})
	r.trimLocked(at)
}

func (r *Recorder) trimLocked(now time.Time) {
	cutoff := now.Add(-r.retain)
	for len(r.blocks) > 0 && r.blocks[0].at.Before(cutoff) {
		blk := r.blocks[0]
		for _, id := range blk.txids {
			if rec, ok := r.txs[id]; ok && rec.at.Before(cutoff) {
				delete(r.txs, id)
			}
		}
		r.blocks = r.blocks[1:]
	}
	for id, rec := range r.txs {
		if rec.at.Before(cutoff) {
			delete(r.txs, id)
		}
	}
}

func matchesPrefix(txid, prefix string) bool {
	return prefix == "" || strings.HasPrefix(txid, prefix)
}

// Lookup returns committed_at for known txids (missing txids omitted).
func (r *Recorder) Lookup(txids []string) map[string]time.Time {
	out := make(map[string]time.Time, len(txids))
	if r == nil || !r.enabled {
		return out
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, id := range txids {
		if rec, ok := r.txs[id]; ok {
			out[id] = rec.at
		}
	}
	return out
}
