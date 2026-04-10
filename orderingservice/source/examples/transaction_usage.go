package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"raft-order-service/internal/types"
	"raft-order-service/pkg/client"
)

// makeDemoTx builds a minimal demo transaction with a deterministic Txid.
func makeDemoTx(label string, n int64) types.Transaction {
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

	raw := make([]byte, 8)
	binary.LittleEndian.PutUint64(raw, uint64(n))
	raw = append(raw, []byte(label)...)
	h1 := sha256.Sum256(raw)
	h2 := sha256.Sum256(h1[:])
	rev := make([]byte, 32)
	for i := range h2 {
		rev[i] = h2[31-i]
	}
	tx.Txid = hex.EncodeToString(rev)

	return tx
}

// Example demonstrating the blockchain transaction architecture
func main() {
	ctx := context.Background()

	orderClient, err := client.NewOrderClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer orderClient.Stop()

	nodeAddr := "/ip4/127.0.0.1/tcp/9001"
	if err := orderClient.ConnectToNode(nodeAddr); err != nil {
		log.Fatalf("Failed to connect to node: %v", err)
	}

	nodes, err := orderClient.GetClusterNodes(orderClient.Transport.ID())
	if err != nil {
		log.Fatalf("Failed to get cluster nodes: %v", err)
	}

	if len(nodes) == 0 {
		log.Fatal("No nodes available")
	}

	targetNode := nodes[0]

	// Example 1: Submit a single transaction
	fmt.Println("=== Example 1: Single Transaction ===")
	tx1 := makeDemoTx("transfer-asset-001", 1)
	txID1, err := orderClient.SubmitTransaction(tx1, targetNode)
	if err != nil {
		log.Printf("Failed to submit tx: %v", err)
	} else {
		fmt.Printf("✅ Transaction submitted! TX ID: %s\n\n", txID1)
	}

	time.Sleep(1 * time.Second)

	// Example 2: Another transaction
	fmt.Println("=== Example 2: Another Transaction ===")
	tx2 := makeDemoTx("transfer-asset-002", 2)
	txID2, err := orderClient.SubmitTransaction(tx2, targetNode)
	if err != nil {
		log.Printf("Failed to submit tx: %v", err)
	} else {
		fmt.Printf("✅ Transaction submitted! TX ID: %s\n\n", txID2)
	}

	time.Sleep(1 * time.Second)

	// Example 3: Batch submit using SubmitTransactionFast
	fmt.Println("=== Example 3: Batch Transactions (Fast Mode) ===")
	for i := int64(0); i < 5; i++ {
		tx := makeDemoTx(fmt.Sprintf("batch-%d", i), 100+i)
		txID, err := orderClient.SubmitTransactionFast(tx, targetNode)
		if err != nil {
			log.Printf("Failed to submit tx %d: %v", i, err)
		} else {
			fmt.Printf("  [%d] TX %s submitted\n", i+1, txID)
		}
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Println("\n✅ All transactions submitted!")
	fmt.Println("Check the server logs to see transaction ordering.")
}
