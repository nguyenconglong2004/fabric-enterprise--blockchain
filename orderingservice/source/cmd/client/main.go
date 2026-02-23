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

	"raft-order-service/pkg/client"
)

func printHelp() {
	fmt.Println("\n=== Commands ===")
	fmt.Println("  order <data>    - Submit a single order")
	fmt.Println("  start [tps]     - Start auto-send (integers, default 1 TPS)")
	fmt.Println("  stop            - Stop auto-send")
	fmt.Println("  speed <tps>     - Change TPS in real-time (while running)")
	fmt.Println("  status          - Show auto-send statistics")
	fmt.Println("  help            - Show this message")
	fmt.Println("  quit            - Exit")
	fmt.Println()
}

func main() {
	fmt.Println("=== Raft Order Service Client ===")
	fmt.Println()

	ctx := context.Background()
	orderClient, err := client.NewOrderClient(ctx)
	if err != nil {
		fmt.Printf("Error creating client: %v\n", err)
		return
	}
	defer orderClient.Stop()

	reader := bufio.NewReader(os.Stdin)

	// Get node address to connect to
	fmt.Print("Enter address of a node in the cluster (e.g., /ip4/127.0.0.1/tcp/6000/p2p/...): ")
	nodeAddr, _ := reader.ReadString('\n')
	nodeAddr = strings.TrimSpace(nodeAddr)

	if nodeAddr == "" {
		fmt.Println("Node address is required")
		return
	}

	// Connect to node
	if err := orderClient.ConnectToNode(nodeAddr); err != nil {
		fmt.Printf("Error connecting to node: %v\n", err)
		return
	}

	// Parse node ID from address
	addr, err := peer.AddrInfoFromString(nodeAddr)
	if err != nil {
		fmt.Printf("Error parsing node address: %v\n", err)
		return
	}

	// Wait a bit for peerstore to populate
	time.Sleep(2 * time.Second)

	// Get list of all nodes in cluster
	fmt.Println("Discovering cluster nodes...")
	allNodes, err := orderClient.GetClusterNodes(addr.ID)
	if err != nil {
		fmt.Printf("Warning: Could not get full cluster list: %v\n", err)
		allNodes = []peer.AddrInfo{*addr}
	}
	fmt.Printf("Found %d node(s) in cluster\n", len(allNodes))

	printHelp()

	// --- Auto-send state ---
	var txCounter int64  // content of each transaction (increments)
	var sendCount int64  // total transactions sent successfully
	var autoRunning bool
	var stopChan chan struct{}
	var speedChan chan float64

	startAuto := func(tps float64) {
		if autoRunning {
			fmt.Println("Auto-send already running. Use 'speed <tps>' to change rate, or 'stop' first.")
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

		fmt.Printf("Auto-send started at %.2f TPS. Use 'stop' to halt, 'speed <n>' to adjust.\n", tps)

		go func() {
			interval := time.Duration(float64(time.Second) / tps)
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			// Periodic stats printer (every 5 seconds)
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
					data := strconv.FormatInt(n, 10)
					_, sendErr := orderClient.SubmitOrderFast(data, allNodes)
					if sendErr != nil {
						fmt.Printf("\n[Auto] Error tx#%d: %v\n> ", n, sendErr)
					} else {
						atomic.AddInt64(&sendCount, 1)
						fmt.Printf("\r[Auto] Sent tx#%d (total sent: %d)", n, atomic.LoadInt64(&sendCount))
					}
				}
			}
		}()
	}

	// Command loop
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
		case "order":
			if len(parts) < 2 {
				fmt.Println("Usage: order <data>")
				continue
			}
			orderData := strings.Join(parts[1:], " ")
			orderID, err := orderClient.SubmitOrder(orderData, allNodes)
			if err != nil {
				fmt.Printf("Error submitting order: %v\n", err)
			} else {
				fmt.Printf("Order request sent: %s\n", orderID)
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
			fmt.Printf("Auto-send stopped. Total sent: %d  Acked: %d\n",
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
				fmt.Println("Auto-send is not running. Use 'start [tps]' first.")
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
			fmt.Printf("Unknown command: %s (type 'help' for commands)\n", command)
		}
	}
}
