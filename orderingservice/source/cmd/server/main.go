package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"raft-order-service/internal/raft"
)

func main() {
	fmt.Println("=== Raft-based Order Service ===")
	fmt.Println("This service uses priority-based leader succession instead of elections")
	fmt.Println()

	// Get port from user
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter port for this node (e.g., 6000): ")
	portStr, _ := reader.ReadString('\n')
	portStr = strings.TrimSpace(portStr)

	var port int
	fmt.Sscanf(portStr, "%d", &port)
	if port == 0 {
		port = 6000
	}

	// Create node
	ctx := context.Background()
	node, err := raft.NewRaftNode(ctx, port)
	if err != nil {
		fmt.Printf("Error creating node: %v\n", err)
		return
	}
	defer node.Stop()

	// Start node
	node.Start()

	fmt.Printf("\nNode started successfully!\n")
	fmt.Printf("Node ID: %s\n", node.ID().ShortString())
	fmt.Printf("Address: %s\n", node.GetAddress())
	fmt.Println()

	// Ask if this is the first node or wants to join existing network
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
				// Wait a bit for membership to sync
				time.Sleep(2 * time.Second)
			}
		}
	}

	// Interactive menu
	fmt.Println("\n=== Commands ===")
	fmt.Println("1. status          - Show node status")
	fmt.Println("2. order <data>    - Submit an order")
	fmt.Println("3. orders          - List all orders")
	fmt.Println("4. propose <i> ... - Propose a block with given transaction indices (leader only)")
	fmt.Println("5. autoblock start - Start auto-propose (3 tx/block, leader only)")
	fmt.Println("6. autoblock stop  - Stop auto-propose")
	fmt.Println("7. connect <addr>  - Connect to another node")
	fmt.Println("8. quit            - Exit")
	fmt.Println()

	// Command loop
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

		case "order":
			if len(parts) < 2 {
				fmt.Println("Usage: order <data>")
				continue
			}
			orderData := parts[1]
			orderID, err := node.SubmitOrder(orderData)
			if err != nil {
				fmt.Printf("Error submitting order: %v\n", err)
			} else {
				fmt.Printf("Order submitted: %s\n", orderID)
			}

		case "orders":
			orders := node.GetOrders()
			if len(orders) == 0 {
				fmt.Println("No orders yet")
			} else {
				fmt.Printf("Total orders: %d\n", len(orders))
				for i, order := range orders {
					fmt.Printf("  %d. %s: %s (status: %s)\n",
						i+1, order.ID, order.Data, order.Status)
				}
			}

		case "propose":
			if !node.IsLeader() {
				fmt.Println("Error: Only leader can propose blocks")
				continue
			}
			if len(parts) < 2 {
				fmt.Println("Usage: propose <index1> [index2] ... (e.g., propose 1 3 4)")
				continue
			}
			// Parse indices
			indicesStr := strings.Fields(parts[1])
			indices := make([]int, 0, len(indicesStr))
			for _, idxStr := range indicesStr {
				var idx int
				if _, err := fmt.Sscanf(idxStr, "%d", &idx); err != nil {
					fmt.Printf("Error: Invalid index '%s'. Must be a number\n", idxStr)
					continue
				}
				indices = append(indices, idx)
			}
			if len(indices) == 0 {
				fmt.Println("Error: No valid indices provided")
				continue
			}
			if err := node.ProposeBlock(indices); err != nil {
				fmt.Printf("Error proposing block: %v\n", err)
			} else {
				fmt.Printf("Block proposal sent with %d transaction(s)\n", len(indices))
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
					fmt.Printf("Auto-propose started (%d tx/block). Use 'autoblock stop' to halt.\n",
						raft.AutoProposeBlockSize)
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
			peerAddr := parts[1]
			if err := node.ConnectToPeer(peerAddr); err != nil {
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
			fmt.Println("1. status          - Show node status")
			fmt.Println("2. order <data>    - Submit an order")
			fmt.Println("3. orders          - List all orders")
			fmt.Println("4. propose <i> ... - Propose a block with given transaction indices (leader only)")
			fmt.Println("5. autoblock start - Start auto-propose (3 tx/block, leader only)")
			fmt.Println("6. autoblock stop  - Stop auto-propose")
			fmt.Println("7. connect <addr>  - Connect to another node")
			fmt.Println("8. quit            - Exit")
			fmt.Println()

		default:
			fmt.Printf("Unknown command: %s (type 'help' for commands)\n", command)
		}
	}
}
