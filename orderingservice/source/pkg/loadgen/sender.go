package loadgen

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	netpkg "raft-order-service/internal/network"
	"raft-order-service/internal/types"
)

// SendEndorsement delivers one smart-contract tx to the orderer leader (endorsement protocol).
func SendEndorsement(transport *netpkg.Transport, leader peer.AddrInfo, tx types.Transaction) error {
	if err := transport.Host.Connect(transport.Ctx, leader); err != nil {
		return err
	}
	return transport.SendEndorsement(leader.ID, tx)
}

// SendStats tracks submission outcomes.
type SendStats struct {
	Sent   int64
	Failed int64
}

// RunSender pumps transactions at targetTPS until ctx is cancelled, then waits for workers.
// targetTPS <= 0 means send as fast as possible.
func RunSender(
	ctx context.Context,
	transport *netpkg.Transport,
	leader peer.AddrInfo,
	opts TxOptions,
	targetTPS int,
	workers int,
	stats *SendStats,
) {
	if workers <= 0 {
		workers = 4
	}
	if stats == nil {
		stats = &SendStats{}
	}

	type job struct {
		seq int64
	}
	jobs := make(chan job, workers*8)

	var wg sync.WaitGroup
	workerFn := func() {
		defer wg.Done()
		for j := range jobs {
			tx, err := NewSmartContractTx(j.seq, opts)
			if err != nil {
				atomic.AddInt64(&stats.Failed, 1)
				continue
			}
			if err := SendEndorsement(transport, leader, tx); err != nil {
				atomic.AddInt64(&stats.Failed, 1)
				continue
			}
			atomic.AddInt64(&stats.Sent, 1)
		}
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go workerFn()
	}

	var seq int64

	if targetTPS <= 0 {
		go func() {
			defer close(jobs)
			for {
				select {
				case <-ctx.Done():
					return
				default:
					seq++
					select {
					case jobs <- job{seq: seq}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
		<-ctx.Done()
		wg.Wait()
		return
	}

	// 10 ticks/s; each tick enqueues targetTPS/10 jobs (min 1).
	perTick := targetTPS / 10
	if perTick < 1 {
		perTick = 1
	}
	ticksPerSec := targetTPS / perTick
	if ticksPerSec < 1 {
		ticksPerSec = 1
	}
	interval := time.Second / time.Duration(ticksPerSec)

	go func() {
		defer close(jobs)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for i := 0; i < perTick; i++ {
					seq++
					select {
					case jobs <- job{seq: seq}:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	<-ctx.Done()
	wg.Wait()
}
