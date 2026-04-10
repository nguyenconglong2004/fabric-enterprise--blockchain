package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chzyer/readline"
	"github.com/libp2p/go-libp2p/core/peer"

	"raft-order-service/internal/types"
	"raft-order-service/pkg/client"
)

func printHelp(out io.Writer) {
	fmt.Fprintln(out, "\n=== Commands ===")
	fmt.Fprintln(out, "  tx <data>    - Submit a single transaction")
	fmt.Fprintln(out, "  start [tps]  - Start auto-send (default 1 TPS)")
	fmt.Fprintln(out, "  stop         - Stop auto-send")
	fmt.Fprintln(out, "  speed <tps>  - Change TPS in real-time")
	fmt.Fprintln(out, "  status       - Show auto-send statistics")
	fmt.Fprintln(out, "  help         - Show this message")
	fmt.Fprintln(out, "  quit         - Exit")
	fmt.Fprintln(out)
}

// makeDemoTx builds a minimal demo transaction with a deterministic Txid.
// In production, transactions would be signed by real wallets using Ed25519.
func makeDemoTx(label string, n int64) types.Transaction {
	// Build a dummy scriptPubKey representing a P2PKH output
	spk := types.ScriptPubKey{
		ASM:       "OP_DUP OP_HASH160 <demo> OP_EQUALVERIFY OP_CHECKSIG",
		Hex:       "76a914" + fmt.Sprintf("%040x", n) + "88ac",
		Addresses: []string{fmt.Sprintf("demo-addr-%d", n)},
	}

	tx := types.Transaction{
		Version: 1,
		Vin: []types.VIN{
			{
				Txid: fmt.Sprintf("%064x", n),
				Vout: 0,
				ScriptSig: types.ScriptSig{
					ASM: label,
					Hex: "",
				},
			},
		},
		Vout: []types.VOUT{
			{
				Value:        int64(n * 100),
				N:            0,
				ScriptPubKey: spk,
			},
		},
		LockTime: 0,
	}

	// Compute a deterministic Txid from the label + n
	raw := make([]byte, 8)
	binary.LittleEndian.PutUint64(raw, uint64(n))
	raw = append(raw, []byte(label)...)
	h1 := sha256.Sum256(raw)
	h2 := sha256.Sum256(h1[:])
	// reverse for display (Bitcoin convention)
	rev := make([]byte, 32)
	for i := range h2 {
		rev[i] = h2[31-i]
	}
	tx.Txid = hex.EncodeToString(rev)

	return tx
}

func main() {
	fmt.Println("=== Raft Ordering Service Client ===")
	fmt.Println()

	ctx := context.Background()
	orderClient, err := client.NewOrderClient(ctx)
	if err != nil {
		fmt.Printf("Error creating client: %v\n", err)
		return
	}
	defer orderClient.Stop()

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          "> ",
		HistoryFile:     "/tmp/raft-client-history.tmp",
		InterruptPrompt: "^C",
		EOFPrompt:       "quit",
	})
	if err != nil {
		fmt.Printf("Error creating readline: %v\n", err)
		return
	}
	defer rl.Close()

	log.SetOutput(rl.Stdout())
	out := rl.Stdout()

	fmt.Fprint(out, "Enter address of a node in the cluster (e.g., /ip4/127.0.0.1/tcp/6000/p2p/...): ")
	rl.SetPrompt("")
	nodeAddr, err := rl.Readline()
	rl.SetPrompt("> ")
	if err != nil {
		return
	}
	nodeAddr = strings.TrimSpace(nodeAddr)

	if nodeAddr == "" {
		fmt.Fprintln(out, "Node address is required")
		return
	}

	if err := orderClient.ConnectToNode(nodeAddr); err != nil {
		fmt.Fprintf(out, "Error connecting to node: %v\n", err)
		return
	}

	addr, err := peer.AddrInfoFromString(nodeAddr)
	if err != nil {
		fmt.Fprintf(out, "Error parsing node address: %v\n", err)
		return
	}

	time.Sleep(2 * time.Second)

	fmt.Fprintln(out, "Discovering cluster nodes...")
	allNodes, err := orderClient.GetClusterNodes(addr.ID)
	if err != nil {
		fmt.Fprintf(out, "Warning: Could not get full cluster list: %v\n", err)
		allNodes = []peer.AddrInfo{*addr}
	}
	fmt.Fprintf(out, "Found %d node(s) in cluster\n", len(allNodes))

	targetNode := allNodes[0]

	printHelp(out)

	var txCounter int64
	var sendCount int64
	var autoRunning bool
	var stopChan chan struct{}
	var speedChan chan float64

	startAuto := func(tps float64) {
		if autoRunning {
			fmt.Fprintln(out, "Auto-send already running.")
			return
		}
		if tps <= 0 {
			fmt.Fprintln(out, "TPS must be > 0.")
			return
		}

		autoRunning = true
		orderClient.AutoMode = true
		stopChan = make(chan struct{})
		speedChan = make(chan float64, 1)

		fmt.Fprintf(out, "Auto-send started at %.2f TPS.\n", tps)

		go func() {
			interval := time.Duration(float64(time.Second) / tps)
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			statsTicker := time.NewTicker(5 * time.Second)
			defer statsTicker.Stop()

			for {
				select {
				case <-stopChan:
					return

				case newTPS := <-speedChan:
					ticker.Reset(time.Duration(float64(time.Second) / newTPS))
					fmt.Fprintf(out, "[Auto] Speed changed to %.2f TPS\n", newTPS)

				case <-statsTicker.C:
					sent := atomic.LoadInt64(&sendCount)
					recv := atomic.LoadInt64(&orderClient.AutoRecvCount)
					fmt.Fprintf(out, "[Auto] Stats: sent=%d  acked=%d\n", sent, recv)

				case <-ticker.C:
					n := atomic.AddInt64(&txCounter, 1)
					tx := makeDemoTx("auto", n)
					_, sendErr := orderClient.SubmitTransactionFast(tx, targetNode)
					if sendErr != nil {
						fmt.Fprintf(out, "[Auto] Error tx#%d: %v\n", n, sendErr)
					} else {
						atomic.AddInt64(&sendCount, 1)
						fmt.Fprintf(out, "[Auto] Sent tx#%d %s (total: %d)\n",
							n, tx.Txid[:8], atomic.LoadInt64(&sendCount))
					}
				}
			}
		}()
	}

	for {
		input, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt || err == io.EOF {
				break
			}
			continue
		}
		input = strings.TrimSpace(input)

		if input == "" {
			continue
		}

		parts := strings.Fields(input)
		command := strings.ToLower(parts[0])

		switch command {
		case "tx":
			if len(parts) < 2 {
				fmt.Fprintln(out, "Usage: tx <label>")
				continue
			}
			label := strings.Join(parts[1:], " ")
			n := atomic.AddInt64(&txCounter, 1)
			tx := makeDemoTx(label, n)

			txID, err := orderClient.SubmitTransaction(tx, targetNode)
			if err != nil {
				fmt.Fprintf(out, "Error submitting transaction: %v\n", err)
			} else {
				fmt.Fprintf(out, "Transaction sent: %s\n", txID)
			}

		case "start":
			tps := 1.0
			if len(parts) >= 2 {
				v, parseErr := strconv.ParseFloat(parts[1], 64)
				if parseErr != nil || v <= 0 {
					fmt.Fprintln(out, "Invalid TPS. Usage: start [tps]")
					continue
				}
				tps = v
			}
			startAuto(tps)

		case "stop":
			if !autoRunning {
				fmt.Fprintln(out, "Auto-send is not running.")
				continue
			}
			close(stopChan)
			autoRunning = false
			orderClient.AutoMode = false
			fmt.Fprintf(out, "Auto-send stopped. Sent: %d  Acked: %d\n",
				atomic.LoadInt64(&sendCount),
				atomic.LoadInt64(&orderClient.AutoRecvCount))

		case "speed":
			if len(parts) < 2 {
				fmt.Fprintln(out, "Usage: speed <tps>")
				continue
			}
			tps, parseErr := strconv.ParseFloat(parts[1], 64)
			if parseErr != nil || tps <= 0 {
				fmt.Fprintln(out, "Invalid TPS value.")
				continue
			}
			if !autoRunning {
				fmt.Fprintln(out, "Auto-send is not running.")
				continue
			}
			speedChan <- tps

		case "status":
			state := "STOPPED"
			if autoRunning {
				state = "RUNNING"
			}
			fmt.Fprintf(out, "Auto-send: %s | TX counter: %d | Sent: %d | Acked: %d\n",
				state,
				atomic.LoadInt64(&txCounter),
				atomic.LoadInt64(&sendCount),
				atomic.LoadInt64(&orderClient.AutoRecvCount))

		case "quit", "exit":
			if autoRunning {
				close(stopChan)
			}
			fmt.Fprintln(out, "Shutting down...")
			return

		case "help":
			printHelp(out)

		default:
			fmt.Fprintf(out, "Unknown command: %s (type 'help')\n", command)
		}
	}
}
