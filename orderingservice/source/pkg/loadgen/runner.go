package loadgen

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	netpkg "raft-order-service/internal/network"
)

// Options configures a load test run.
type Options struct {
	OrdererAddr    string
	ContractName   string
	FunctionName   string
	TxPrefix       string
	ClientPubKey   string
	TPS            int
	Duration       time.Duration
	DrainWait      time.Duration
	Workers        int
	OrchestratorWS string
	ProgressEvery  time.Duration
}

// Result holds counters after a run.
type Result struct {
	LoadStart time.Time
	LoadEnd   time.Time
	DrainEnd  time.Time
	Send      SendStats
	Commit    *CommitStats
	Leader    string
}

// Run executes the load generator.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.OrdererAddr == "" {
		return nil, fmt.Errorf("orderer multiaddr is required")
	}
	if opts.Duration <= 0 {
		opts.Duration = 30 * time.Second
	}
	if opts.DrainWait <= 0 {
		opts.DrainWait = 15 * time.Second
	}
	if opts.Workers <= 0 {
		opts.Workers = 8
	}
	if opts.TxPrefix == "" {
		opts.TxPrefix = "loadgen-"
	}
	if opts.ProgressEvery <= 0 {
		opts.ProgressEvery = 5 * time.Second
	}

	transport, err := netpkg.NewClientTransport(ctx)
	if err != nil {
		return nil, fmt.Errorf("create transport: %w", err)
	}
	defer transport.Close()

	leader, err := ResolveLeader(ctx, transport, opts.OrdererAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve leader: %w", err)
	}
	leaderAI := peer.AddrInfo{ID: leader.Leader, Addrs: leader.Addrs}
	if err := transport.Host.Connect(ctx, leaderAI); err != nil {
		return nil, fmt.Errorf("connect leader: %w", err)
	}
	fmt.Printf("→ Leader: %s\n", leader.AddrStr)

	txOpts := TxOptions{
		Prefix:       opts.TxPrefix,
		ContractName: opts.ContractName,
		FunctionName: opts.FunctionName,
		ClientPubKey: opts.ClientPubKey,
	}

	var commitStats *CommitStats
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if opts.OrchestratorWS != "" {
		commitStats = &CommitStats{}
		go func() {
			if err := WatchOrchestratorWS(runCtx, opts.OrchestratorWS, commitStats); err != nil && runCtx.Err() == nil {
				fmt.Printf("⚠️  orchestrator WS: %v\n", err)
			}
		}()
	}

	sendStats := &SendStats{}
	StartProgressLogger(runCtx, opts.ProgressEvery, &sendStats.Sent, &sendStats.Failed, commitStats)

	fmt.Printf("→ Load: %d tx/s × %s (%d workers) prefix=%s contract=%s\n",
		opts.TPS, opts.Duration, opts.Workers, opts.TxPrefix, txOpts.ContractName)

	loadStart := time.Now().UTC()
	loadCtx, loadCancel := context.WithTimeout(runCtx, opts.Duration)
	RunSender(loadCtx, transport, leaderAI, txOpts, opts.TPS, opts.Workers, sendStats)
	loadCancel()
	loadEnd := time.Now().UTC()
	fmt.Printf("→ Load done: sent=%d failed=%d\n", sendStats.Sent, sendStats.Failed)

	fmt.Printf("\nLoad finished. Draining %s for in-flight blocks...\n", opts.DrainWait)
	select {
	case <-time.After(opts.DrainWait):
	case <-ctx.Done():
	}
	drainEnd := time.Now().UTC()
	cancel()

	result := &Result{
		LoadStart: loadStart,
		LoadEnd:   loadEnd,
		DrainEnd:  drainEnd,
		Send:      *sendStats,
		Commit:    commitStats,
		Leader:    leader.AddrStr,
	}

	PrintSummary("ORDERER LOADGEN", loadStart, loadEnd, drainEnd, sendStats.Sent, sendStats.Failed, commitStats)
	return result, nil
}
