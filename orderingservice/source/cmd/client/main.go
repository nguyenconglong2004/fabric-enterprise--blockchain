package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"raft-order-service/pkg/client"
)

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
		// Fallback to just the connected node
		allNodes = []peer.AddrInfo{*addr}
	}
	fmt.Printf("Found %d node(s) in cluster\n", len(allNodes))

	fmt.Println("\n=== Commands ===")
	fmt.Println("1. order <data> - Submit an order to all nodes (e.g., order Buy 100 BTC)")
	fmt.Println("2. quit - Exit")
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
		case "order":
			if len(parts) < 2 {
				fmt.Println("Usage: order <data>")
				continue
			}
			orderData := parts[1]
			orderID, err := orderClient.SubmitOrder(orderData, allNodes)
			if err != nil {
				fmt.Printf("Error submitting order: %v\n", err)
			} else {
				fmt.Printf("Order request sent to all nodes: %s (waiting for response...)\n", orderID)
			}

		case "quit", "exit":
			fmt.Println("Shutting down...")
			return

		case "help":
			fmt.Println("\n=== Commands ===")
			fmt.Println("1. order <data> - Submit an order")
			fmt.Println("2. quit - Exit")
			fmt.Println()

		default:
			fmt.Printf("Unknown command: %s (type 'help' for commands)\n", command)
		}
	}
}
