package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"raft-order-service/internal/raft"
)

func main() {
	fmt.Println("=== Raft-based Ordering Service ===")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter port for this node (e.g., 6000): ")
	portStr, _ := reader.ReadString('\n')
	portStr = strings.TrimSpace(portStr)

	var port int
	fmt.Sscanf(portStr, "%d", &port)
	if port == 0 {
		port = 6000
	}

	ctx := context.Background()
	node, err := raft.NewRaftNode(ctx, port)
	if err != nil {
		fmt.Printf("Error creating node: %v\n", err)
		return
	}
	defer node.Stop()

	node.Start()

	fmt.Printf("\nNode started!\n")
	fmt.Printf("Node ID: %s\n", node.ID().ShortString())
	fmt.Printf("Address: %s\n", node.GetAddress())
	fmt.Println()

	fmt.Print("Is this the first node? (y/n): ")
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	if response != "y" && response != "yes" {
		fmt.Print("Enter address of existing node to connect to: ")
		peerAddr, _ := reader.ReadString('\n')
		peerAddr = strings.TrimSpace(peerAddr)

		if peerAddr != "" {
			if err := node.ConnectToPeer(peerAddr); err != nil {
				fmt.Printf("Error connecting to peer: %v\n", err)
			} else {
				time.Sleep(2 * time.Second)
			}
		}
	}

	fmt.Println("\n=== Commands ===")
	fmt.Println("  status              - Show node status (membership, raft log, ordering blocks)")
	fmt.Println("  propose [n]         - Propose a block with first n tx from pool (default 3)")
	fmt.Println("  autoblock start     - Start auto-propose blocks")
	fmt.Println("  autoblock stop      - Stop auto-propose blocks")
	fmt.Println("  connect <addr>      - Connect to another node")
	fmt.Println("  quit                - Exit")
	fmt.Println()

	for {
		fmt.Print("> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" {
			continue
		}

		parts := strings.SplitN(input, " ", 2)
		command := strings.ToLower(parts[0])

		switch command {
		case "status":
			node.PrintStatus()

		case "propose":
			if !node.IsLeader() {
				fmt.Println("Error: only leader can propose blocks")
				continue
			}
			n := raft.AutoProposeBlockSize
			if len(parts) >= 2 {
				v, err := strconv.Atoi(strings.TrimSpace(parts[1]))
				if err != nil || v < 1 {
					fmt.Println("Usage: propose [n]  (n = max number of tx, default 3)")
					continue
				}
				n = v
			}
			if err := node.ProposeBlock(n); err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				fmt.Printf("Block proposal sent\n")
			}

		case "autoblock":
			if len(parts) < 2 {
				fmt.Println("Usage: autoblock start | autoblock stop")
				continue
			}
			sub := strings.ToLower(strings.TrimSpace(parts[1]))
			switch sub {
			case "start":
				if !node.IsLeader() {
					fmt.Println("Error: only leader can auto-propose blocks")
					continue
				}
				if err := node.StartAutoProposeBlock(raft.AutoProposeBlockSize); err != nil {
					fmt.Printf("Error: %v\n", err)
				} else {
					fmt.Printf("Auto-propose started (%d tx/block).\n", raft.AutoProposeBlockSize)
				}
			case "stop":
				if !node.IsAutoProposeRunning() {
					fmt.Println("Auto-propose is not running.")
					continue
				}
				node.StopAutoProposeBlock()
				fmt.Println("Auto-propose stopped.")
			default:
				fmt.Println("Usage: autoblock start | autoblock stop")
			}

		case "connect":
			if len(parts) < 2 {
				fmt.Println("Usage: connect <address>")
				continue
			}
			if err := node.ConnectToPeer(parts[1]); err != nil {
				fmt.Printf("Error connecting: %v\n", err)
			} else {
				fmt.Println("Connected successfully")
			}

		case "quit", "exit":
			node.StopAutoProposeBlock()
			fmt.Println("Shutting down...")
			return

		case "help":
			fmt.Println("\n=== Commands ===")
			fmt.Println("  status              - Show node status")
			fmt.Println("  propose <i> [i...]  - Propose a block with tx at given indices")
			fmt.Println("  autoblock start     - Start auto-propose blocks")
			fmt.Println("  autoblock stop      - Stop auto-propose blocks")
			fmt.Println("  connect <addr>      - Connect to another node")
			fmt.Println("  quit                - Exit")
			fmt.Println()

		default:
			fmt.Printf("Unknown command: %s (type 'help' for commands)\n", command)
		}
	}
}
