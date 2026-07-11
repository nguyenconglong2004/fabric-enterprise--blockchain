// bench runs an in-process parameter sweep of the ordering service using the
// loadgen library. For each configuration it spins up a fresh single-node Raft
// leader (self-majority => commit path isolated from multi-node consensus RTT),
// drives synthetic smart-contract transactions through the endorsement path,
// and measures block-commit throughput via the deliver stream.
//
// It prints a results table and writes a CSV (default: bench_results.csv).
//
// Usage:
//
//	go run ./cmd/bench -out bench_results.csv
//	go run ./cmd/bench -sweep all -load 15s -drain 8s
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"raft-order-service/internal/raft"
	"raft-order-service/pkg/loadgen"
)

// scenario is one benchmark run configuration.
type scenario struct {
	Group      string // sweep group name
	BlockSize  int    // auto_propose_block_size
	IntervalMs int    // auto_propose_interval_ms
	TPS        int    // target send tx/s (0 = unlimited)
	Workers    int    // parallel send workers
}

// row is one measured result.
type row struct {
	scenario
	SentRate    float64
	Failed      int64
	LoadBlocks  int64
	LoadTxs     int64
	LoadBlkPerS float64
	LoadTxPerS  float64
	AvgTxBlock  float64
	FullTxPerS  float64 // load+drain sustained
}

func main() {
	out := flag.String("out", "bench_results.csv", "CSV output path")
	sweep := flag.String("sweep", "all", "which sweep: all|tps|blocksize|interval|workers")
	loadStr := flag.String("load", "15s", "load duration per run")
	drainStr := flag.String("drain", "8s", "drain duration per run")
	basePort := flag.Int("base-port", 7000, "starting P2P port (incremented per run)")
	flag.Parse()

	loadDur, err := time.ParseDuration(*loadStr)
	if err != nil {
		log.Fatalf("bad -load: %v", err)
	}
	drainDur, err := time.ParseDuration(*drainStr)
	if err != nil {
		log.Fatalf("bad -drain: %v", err)
	}

	scenarios := buildScenarios(*sweep)
	fmt.Printf("=== Ordering Service benchmark: %d runs, load=%s drain=%s ===\n\n",
		len(scenarios), loadDur, drainDur)

	var rows []row
	for i, sc := range scenarios {
		port := *basePort + i
		fmt.Printf("[%d/%d] group=%s block=%d interval=%dms tps=%d workers=%d (port %d)\n",
			i+1, len(scenarios), sc.Group, sc.BlockSize, sc.IntervalMs, sc.TPS, sc.Workers, port)
		r, err := runOne(port, sc, loadDur, drainDur)
		if err != nil {
			fmt.Printf("    ERROR: %v\n", err)
			continue
		}
		fmt.Printf("    -> send=%.0f tx/s failed=%d | commit=%.0f tx/s %.2f blk/s avg=%.0f tx/blk | full=%.0f tx/s\n\n",
			r.SentRate, r.Failed, r.LoadTxPerS, r.LoadBlkPerS, r.AvgTxBlock, r.FullTxPerS)
		rows = append(rows, r)
		time.Sleep(2 * time.Second) // let the OS release the port / goroutines settle
	}

	if err := writeCSV(*out, rows); err != nil {
		log.Fatalf("write csv: %v", err)
	}
	fmt.Printf("Wrote %d rows to %s\n", len(rows), *out)
	printTable(rows)
}

// runOne spins up a fresh single-node leader with the given ordering config and
// runs one loadgen pass against it, returning the measured row.
func runOne(port int, sc scenario, loadDur, drainDur time.Duration) (row, error) {
	ctx := context.Background()

	cfg := raft.DefaultConfig()
	cfg.AutoProposeBlockSize = sc.BlockSize
	cfg.AutoProposeInterval = time.Duration(sc.IntervalMs) * time.Millisecond

	// Silence node logging so it does not drown the benchmark output.
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return row{}, fmt.Errorf("open devnull: %w", err)
	}
	defer devnull.Close()
	silent := log.New(devnull, "", 0)

	node, err := raft.NewRaftNode(ctx, port, cfg, raft.NoopEmitter{}, silent)
	if err != nil {
		return row{}, fmt.Errorf("new node: %w", err)
	}
	defer node.Stop()

	node.Start()
	node.BootstrapAsLeader()
	time.Sleep(1500 * time.Millisecond) // let leader + auto-propose loop settle

	opts := loadgen.Options{
		OrdererAddr:   node.GetAddress(),
		ContractName:  "bench_ping",
		FunctionName:  "execute",
		TxPrefix:      fmt.Sprintf("bench-%d-", port),
		ClientPubKey:  loadgen.DefaultClientPubKey,
		TPS:           sc.TPS,
		Duration:      loadDur,
		DrainWait:     drainDur,
		Workers:       sc.Workers,
		ProgressEvery: 5 * time.Second,
	}

	res, err := loadgen.Run(ctx, opts)
	if err != nil {
		return row{}, fmt.Errorf("loadgen: %w", err)
	}

	loadSec := res.LoadEnd.Sub(res.LoadStart).Seconds()
	if loadSec < 0.001 {
		loadSec = 0.001
	}
	fullSec := res.DrainEnd.Sub(res.LoadStart).Seconds()
	if fullSec < 0.001 {
		fullSec = 0.001
	}

	loadBlocks, loadTxs := res.Commit.CountInWindow(res.LoadStart, res.LoadEnd)
	fullBlocks, fullTxs := res.Commit.CountInWindow(res.LoadStart, res.DrainEnd)
	_ = fullBlocks

	r := row{
		scenario:    sc,
		SentRate:    float64(res.Send.Sent) / loadSec,
		Failed:      res.Send.Failed,
		LoadBlocks:  loadBlocks,
		LoadTxs:     loadTxs,
		LoadBlkPerS: float64(loadBlocks) / loadSec,
		LoadTxPerS:  float64(loadTxs) / loadSec,
		FullTxPerS:  float64(fullTxs) / fullSec,
	}
	if loadBlocks > 0 {
		r.AvgTxBlock = float64(loadTxs) / float64(loadBlocks)
	}
	return r, nil
}

// buildScenarios returns the run matrix for the requested sweep.
func buildScenarios(which string) []scenario {
	var s []scenario

	// Sweep 1: find the send/commit saturation point at the default ordering
	// config (block=1000, interval=100ms). Workers fixed high enough not to be
	// the bottleneck.
	tpsSweep := func() {
		for _, tps := range []int{2000, 5000, 10000, 15000, 20000, 0} {
			s = append(s, scenario{"tps", 1000, 100, tps, 32})
		}
	}
	// Sweep 2: block size at saturating load (unlimited tps).
	blockSweep := func() {
		for _, bs := range []int{100, 250, 500, 1000, 2000, 4000} {
			s = append(s, scenario{"blocksize", bs, 100, 0, 32})
		}
	}
	// Sweep 3: propose interval at saturating load.
	intervalSweep := func() {
		for _, iv := range []int{20, 50, 100, 200, 500} {
			s = append(s, scenario{"interval", 1000, iv, 0, 32})
		}
	}
	// Sweep 4: send workers at saturating load.
	workerSweep := func() {
		for _, w := range []int{8, 16, 32, 64} {
			s = append(s, scenario{"workers", 1000, 100, 0, w})
		}
	}
	// Sweep 5: propose interval at a BELOW-saturation load (tps=5000). Here the
	// pool does not fill a full batch within the interval, so the interval — not
	// the event-driven path — governs block rate and batch granularity.
	intervalLoadSweep := func() {
		for _, iv := range []int{20, 50, 100, 200, 500} {
			s = append(s, scenario{"interval-load", 1000, iv, 5000, 32})
		}
	}
	// Sweep 6: send workers at a STABLE saturation point (block=500) to isolate
	// the worker effect away from the block=1000 boundary instability.
	workerStableSweep := func() {
		for _, w := range []int{8, 16, 32, 48, 64} {
			s = append(s, scenario{"workers-stable", 500, 100, 0, w})
		}
	}

	switch which {
	case "tps":
		tpsSweep()
	case "blocksize":
		blockSweep()
	case "interval":
		intervalSweep()
	case "workers":
		workerSweep()
	case "interval-load":
		intervalLoadSweep()
	case "workers-stable":
		workerStableSweep()
	case "extra":
		intervalLoadSweep()
		workerStableSweep()
	default: // all
		tpsSweep()
		blockSweep()
		intervalSweep()
		workerSweep()
	}
	return s
}

func writeCSV(path string, rows []row) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{
		"group", "block_size", "interval_ms", "tps", "workers",
		"send_rate", "failed", "load_blocks", "load_txs",
		"blocks_per_s", "tx_per_s", "avg_tx_block", "full_tx_per_s",
	})
	for _, r := range rows {
		_ = w.Write([]string{
			r.Group,
			strconv.Itoa(r.BlockSize),
			strconv.Itoa(r.IntervalMs),
			strconv.Itoa(r.TPS),
			strconv.Itoa(r.Workers),
			fmt.Sprintf("%.1f", r.SentRate),
			strconv.FormatInt(r.Failed, 10),
			strconv.FormatInt(r.LoadBlocks, 10),
			strconv.FormatInt(r.LoadTxs, 10),
			fmt.Sprintf("%.2f", r.LoadBlkPerS),
			fmt.Sprintf("%.1f", r.LoadTxPerS),
			fmt.Sprintf("%.1f", r.AvgTxBlock),
			fmt.Sprintf("%.1f", r.FullTxPerS),
		})
	}
	return nil
}

func printTable(rows []row) {
	fmt.Println()
	fmt.Printf("%-10s %6s %9s %7s %8s %10s %10s %9s %8s\n",
		"group", "block", "interval", "tps", "workers", "send/s", "commit/s", "blk/s", "avg")
	for _, r := range rows {
		fmt.Printf("%-10s %6d %7dms %7d %8d %10.0f %10.0f %9.2f %8.0f\n",
			r.Group, r.BlockSize, r.IntervalMs, r.TPS, r.Workers,
			r.SentRate, r.LoadTxPerS, r.LoadBlkPerS, r.AvgTxBlock)
	}
}
