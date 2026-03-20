package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"raft-order-service/internal/types"
	"raft-order-service/pkg/client"
)

// Example demonstrating the new transaction architecture
func main() {
	ctx := context.Background()

	// Create client
	orderClient, err := client.NewOrderClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer orderClient.Stop()

	// Connect to a node
	nodeAddr := "/ip4/127.0.0.1/tcp/9001"
	if err := orderClient.ConnectToNode(nodeAddr); err != nil {
		log.Fatalf("Failed to connect to node: %v", err)
	}

	// Get node info
	nodes, err := orderClient.GetClusterNodes(orderClient.Transport.ID())
	if err != nil {
		log.Fatalf("Failed to get cluster nodes: %v", err)
	}

	if len(nodes) == 0 {
		log.Fatal("No nodes available")
	}

	targetNode := nodes[0]

	// Example 1: Submit AssetTransfer transaction
	fmt.Println("=== Example 1: Asset Transfer Transaction ===")

	assetTransferData := map[string]interface{}{
		"ID":        fmt.Sprintf("tx-asset-%d", time.Now().UnixNano()),
		"asset_id":  "ASSET-001",
		"new_owner": "Bob",
		"value":     1000.50,
	}

	txID1, err := orderClient.SubmitTransaction(
		types.TransferType,
		assetTransferData,
		targetNode,
	)
	if err != nil {
		log.Printf("Failed to submit asset transfer: %v", err)
	} else {
		fmt.Printf("✅ Asset transfer submitted successfully! TX ID: %s\n\n", txID1)
	}

	time.Sleep(1 * time.Second)

	// Example 2: Submit another AssetTransfer transaction
	fmt.Println("=== Example 2: Another Asset Transfer ===")

	assetTransferData2 := map[string]interface{}{
		"ID":        fmt.Sprintf("tx-asset-%d", time.Now().UnixNano()),
		"asset_id":  "ASSET-002",
		"new_owner": "Charlie",
		"value":     2500.75,
	}

	txID2, err := orderClient.SubmitTransaction(
		types.TransferType,
		assetTransferData2,
		targetNode,
	)
	if err != nil {
		log.Printf("Failed to submit asset transfer: %v", err)
	} else {
		fmt.Printf("✅ Asset transfer submitted successfully! TX ID: %s\n\n", txID2)
	}

	time.Sleep(1 * time.Second)

	// Example 3: Batch submit using SubmitTransactionFast
	fmt.Println("=== Example 3: Batch Asset Transfers (Fast Mode) ===")

	for i := 0; i < 5; i++ {
		assetData := map[string]interface{}{
			"ID":        fmt.Sprintf("tx-batch-%d", time.Now().UnixNano()),
			"asset_id":  fmt.Sprintf("ASSET-%03d", i+100),
			"new_owner": fmt.Sprintf("Owner-%d", i),
			"value":     float64((i + 1) * 100),
		}

		txID, err := orderClient.SubmitTransactionFast(
			types.TransferType,
			assetData,
			targetNode,
		)
		if err != nil {
			log.Printf("Failed to submit tx %d: %v", i, err)
		} else {
			fmt.Printf("  [%d] TX %s submitted\n", i+1, txID)
		}

		time.Sleep(100 * time.Millisecond)
	}

	fmt.Println("\n✅ All transactions submitted!")
	fmt.Println("Check the server logs to see transaction execution.")
}
