package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"raft-order-service/internal/types"
	"raft-order-service/pkg/client"
)

func printHelp() {
	fmt.Println("\n=== Commands ===")
	fmt.Println("  tx <data>    - Submit a single transaction")
	fmt.Println("  start [tps]  - Start auto-send (default 1 TPS)")
	fmt.Println("  stop         - Stop auto-send")
	fmt.Println("  speed <tps>  - Change TPS in real-time")
	fmt.Println("  status       - Show auto-send statistics")
	fmt.Println("  help         - Show this message")
	fmt.Println("  quit         - Exit")
	fmt.Println()
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

	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter address of a node in the cluster (e.g., /ip4/127.0.0.1/tcp/6000/p2p/...): ")
	nodeAddr, _ := reader.ReadString('\n')
	nodeAddr = strings.TrimSpace(nodeAddr)

	if nodeAddr == "" {
		fmt.Println("Node address is required")
		return
	}

	if err := orderClient.ConnectToNode(nodeAddr); err != nil {
		fmt.Printf("Error connecting to node: %v\n", err)
		return
	}

	addr, err := peer.AddrInfoFromString(nodeAddr)
	if err != nil {
		fmt.Printf("Error parsing node address: %v\n", err)
		return
	}

	time.Sleep(2 * time.Second)

	// Discover cluster and pick a target node (use first node; it will forward to leader)
	fmt.Println("Discovering cluster nodes...")
	allNodes, err := orderClient.GetClusterNodes(addr.ID)
	if err != nil {
		fmt.Printf("Warning: Could not get full cluster list: %v\n", err)
		allNodes = []peer.AddrInfo{*addr}
	}
	fmt.Printf("Found %d node(s) in cluster\n", len(allNodes))

	// Use the first node as target; it will forward to leader if needed
	targetNode := allNodes[0]

	printHelp()

	var txCounter int64
	var sendCount int64
	var autoRunning bool
	var stopChan chan struct{}
	var speedChan chan float64

	startAuto := func(tps float64) {
		if autoRunning {
			fmt.Println("Auto-send already running.")
			return
		}
		if tps <= 0 {
			fmt.Println("TPS must be > 0.")
			return
		}

		autoRunning = true
		orderClient.AutoMode = true
		stopChan = make(chan struct{})
		speedChan = make(chan float64, 1)

		fmt.Printf("Auto-send started at %.2f TPS.\n", tps)

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
					fmt.Printf("\n[Auto] Speed changed to %.2f TPS\n> ", newTPS)

				case <-statsTicker.C:
					sent := atomic.LoadInt64(&sendCount)
					recv := atomic.LoadInt64(&orderClient.AutoRecvCount)
					fmt.Printf("\n[Auto] Stats: sent=%d  acked=%d\n> ", sent, recv)

				case <-ticker.C:
					n := atomic.AddInt64(&txCounter, 1)
					data := map[string]interface{}{
						"ID":        fmt.Sprintf("tx-auto-%d", n),
						"asset_id":  fmt.Sprintf("ASSET-%d", n),
						"new_owner": fmt.Sprintf("Owner-%d", n),
						"value":     float64(n * 100),
					}
					_, sendErr := orderClient.SubmitTransactionFast(types.TransferType, data, targetNode)
					if sendErr != nil {
						fmt.Printf("\n[Auto] Error tx#%d: %v\n> ", n, sendErr)
					} else {
						atomic.AddInt64(&sendCount, 1)
						fmt.Printf("\r[Auto] Sent tx#%d (total: %d)", n, atomic.LoadInt64(&sendCount))
					}
				}
			}
		}()
	}

	for {
		fmt.Print("\n> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" {
			continue
		}

		parts := strings.Fields(input)
		command := strings.ToLower(parts[0])

		switch command {
		case "tx":
			if len(parts) < 2 {
				fmt.Println("Usage: tx <data>")
				continue
			}
			txData := strings.Join(parts[1:], " ")

			// Create a simple asset transfer transaction
			data := map[string]interface{}{
				"ID":        fmt.Sprintf("tx-manual-%d", time.Now().UnixNano()),
				"asset_id":  fmt.Sprintf("ASSET-%s", txData),
				"new_owner": "ManualOwner",
				"value":     100.0,
			}

			txID, err := orderClient.SubmitTransaction(types.TransferType, data, targetNode)
			if err != nil {
				fmt.Printf("Error submitting transaction: %v\n", err)
			} else {
				fmt.Printf("Transaction sent: %s\n", txID)
			}

		case "start":
			tps := 1.0
			if len(parts) >= 2 {
				v, parseErr := strconv.ParseFloat(parts[1], 64)
				if parseErr != nil || v <= 0 {
					fmt.Println("Invalid TPS. Usage: start [tps]")
					continue
				}
				tps = v
			}
			startAuto(tps)

		case "stop":
			if !autoRunning {
				fmt.Println("Auto-send is not running.")
				continue
			}
			close(stopChan)
			autoRunning = false
			orderClient.AutoMode = false
			fmt.Printf("Auto-send stopped. Sent: %d  Acked: %d\n",
				atomic.LoadInt64(&sendCount),
				atomic.LoadInt64(&orderClient.AutoRecvCount))

		case "speed":
			if len(parts) < 2 {
				fmt.Println("Usage: speed <tps>")
				continue
			}
			tps, parseErr := strconv.ParseFloat(parts[1], 64)
			if parseErr != nil || tps <= 0 {
				fmt.Println("Invalid TPS value.")
				continue
			}
			if !autoRunning {
				fmt.Println("Auto-send is not running.")
				continue
			}
			speedChan <- tps

		case "status":
			state := "STOPPED"
			if autoRunning {
				state = "RUNNING"
			}
			fmt.Printf("Auto-send: %s | TX counter: %d | Sent: %d | Acked: %d\n",
				state,
				atomic.LoadInt64(&txCounter),
				atomic.LoadInt64(&sendCount),
				atomic.LoadInt64(&orderClient.AutoRecvCount))

		case "quit", "exit":
			if autoRunning {
				close(stopChan)
			}
			fmt.Println("Shutting down...")
			return

		case "help":
			printHelp()

		default:
			fmt.Printf("Unknown command: %s (type 'help')\n", command)
		}
	}
}
