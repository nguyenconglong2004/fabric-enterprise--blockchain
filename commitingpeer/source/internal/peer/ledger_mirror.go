package peer

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"commiting-peer/internal/types"
)

// ledgerMirrorJob mirrors an already-committed block to PostgreSQL (explorer only).
type ledgerMirrorJob struct {
	block       types.Block
	hashHex     string
	blockNumber int64
	committedAt time.Time
}

func ledgerMirrorEnabled() bool {
	return os.Getenv("COMMIT_PEER_PG_MIRROR") != "0"
}

func ledgerMirrorWorkers() int {
	n := 2
	if s := os.Getenv("COMMIT_PEER_PG_WORKERS"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			n = v
		}
	}
	return n
}

func ledgerMirrorQueueSize() int {
	n := 512
	if s := os.Getenv("COMMIT_PEER_PG_QUEUE"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			n = v
		}
	}
	return n
}

func (p *CommittingPeer) initLedgerMirror() {
	if p.db == nil || !ledgerMirrorEnabled() {
		return
	}
	p.ledgerMirror = make(chan ledgerMirrorJob, ledgerMirrorQueueSize())
}

func (p *CommittingPeer) startLedgerMirror(ctx context.Context) {
	if p.ledgerMirror == nil {
		return
	}
	n := ledgerMirrorWorkers()
	log.Printf("[peer] postgres mirror: async (%d workers, queue %d)", n, cap(p.ledgerMirror))
	for i := 0; i < n; i++ {
		go p.ledgerMirrorWorker(ctx)
	}
}

func (p *CommittingPeer) ledgerMirrorWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-p.ledgerMirror:
			if !ok {
				return
			}
			p.saveBlockToDatabase(job.block, job.hashHex, job.blockNumber, job.committedAt)
		}
	}
}

func (p *CommittingPeer) enqueueLedgerMirror(block types.Block, hashHex string, blockNumber int64, committedAt time.Time) {
	if p.ledgerMirror == nil {
		return
	}
	job := ledgerMirrorJob{
		block:       block,
		hashHex:     hashHex,
		blockNumber: blockNumber,
		committedAt: committedAt,
	}
	select {
	case p.ledgerMirror <- job:
	default:
		log.Printf("[peer] postgres mirror queue full (%d), block %s — increase COMMIT_PEER_PG_QUEUE",
			cap(p.ledgerMirror), hashHex[:min(16, len(hashHex))])
		go p.saveBlockToDatabase(block, hashHex, blockNumber, committedAt)
	}
}
