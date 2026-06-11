// loadgen floods the ordering service with synthetic smart-contract transactions
// via libp2p endorsement protocol (same wire path as Core Service).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"raft-order-service/pkg/loadgen"
)

func main() {
	orderer := flag.String("orderer", "", "Orderer multiaddr (any cluster member), e.g. /ip4/127.0.0.1/tcp/6000/p2p/12D3Koo...")
	tps := flag.Int("tps", 5000, "Target transactions per second (0 = unlimited)")
	duration := flag.String("duration", "30s", "Load duration, e.g. 30s, 2m")
	drain := flag.String("drain", "15s", "Wait after load for blocks to commit")
	workers := flag.Int("workers", 16, "Parallel send workers")
	prefix := flag.String("prefix", "loadgen-", "txid prefix (for filtering metrics)")
	contract := flag.String("contract", "bench_ping", "contract_name field")
	function := flag.String("function", "execute", "function_name field")
	clientPub := flag.String("client-pubkey", loadgen.DefaultClientPubKey, "client_pubkey hex (32-byte Ed25519 pub)")
	ws := flag.String("ws", "", "Orchestrator WebSocket for block-committed metrics (empty = disabled, for cmd/server)")
	progress := flag.String("progress", "5s", "Progress log interval")
	flag.Parse()

	if strings.TrimSpace(*orderer) == "" {
		fmt.Fprintln(os.Stderr, "Error: -orderer is required")
		flag.Usage()
		os.Exit(1)
	}

	dur, err := time.ParseDuration(*duration)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid -duration: %v\n", err)
		os.Exit(1)
	}
	drainWait, err := time.ParseDuration(*drain)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid -drain: %v\n", err)
		os.Exit(1)
	}
	progressEvery, err := time.ParseDuration(*progress)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid -progress: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := loadgen.Options{
		OrdererAddr:    strings.TrimSpace(*orderer),
		ContractName:   *contract,
		FunctionName:   *function,
		TxPrefix:       *prefix,
		ClientPubKey:   strings.TrimSpace(*clientPub),
		TPS:            *tps,
		Duration:       dur,
		DrainWait:      drainWait,
		Workers:        *workers,
		OrchestratorWS: strings.TrimSpace(*ws),
		ProgressEvery:  progressEvery,
	}

	if _, err := loadgen.Run(ctx, opts); err != nil {
		fmt.Fprintf(os.Stderr, "loadgen failed: %v\n", err)
		os.Exit(1)
	}
}
