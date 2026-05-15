package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"commiting-peer/internal/crypto"
	"commiting-peer/internal/deliver"
	peerpkg "commiting-peer/internal/peer"
	"commiting-peer/internal/storage"
	"commiting-peer/internal/validation"
)

// in reads one trimmed line from stdin.
func in(scanner *bufio.Scanner, prompt string) string {
	fmt.Print(prompt)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

func loadOrGenerateEndorsementKey() (privHex, pubHex string, err error) {
	// Try to load from environment variable
	priv := strings.TrimSpace(os.Getenv("COMMIT_PEER_PRIVATE_KEY"))

	// Try to load from file if env var not set
	if priv == "" {
		path := strings.TrimSpace(os.Getenv("COMMIT_PEER_KEY_FILE"))
		if path == "" {
			path = "endorsement.key"
		}
		b, readErr := os.ReadFile(path)
		if readErr == nil {
			priv = strings.TrimSpace(string(b))
		}
	}

	// Generate new key if not found
	if priv == "" {
		fmt.Println("🔑 Generating new Ed25519 endorsement key...")
		kp, genErr := crypto.GenerateKeyPair()
		if genErr != nil {
			return "", "", fmt.Errorf("failed to generate key pair: %w", genErr)
		}
		priv = kp.PrivateKey

		// Save to default file for future use
		path := strings.TrimSpace(os.Getenv("COMMIT_PEER_KEY_FILE"))
		if path == "" {
			path = "endorsement.key"
		}
		if saveErr := os.WriteFile(path, []byte(priv), 0600); saveErr != nil {
			fmt.Printf("⚠️  Warning: could not save key to %s: %v\n", path, saveErr)
		} else {
			fmt.Printf("✅ Saved private key to %s\n", path)
		}
	}

	// Get public key
	pub := strings.TrimSpace(os.Getenv("COMMIT_PEER_PUBLIC_KEY"))
	if pub != "" {
		return priv, pub, nil
	}
	pub, err = crypto.PublicKeyFromPrivateHex(priv)
	return priv, pub, err
}

func main() {
	fmt.Println("=== Committing Peer ===")
	fmt.Println()

	sc := bufio.NewScanner(os.Stdin)

	ordererAddr := in(sc, "Enter orderer address (e.g. /ip4/127.0.0.1/tcp/6000/p2p/<PeerID>): ")
	if ordererAddr == "" {
		fmt.Println("Error: orderer address is required")
		return
	}

	blockFile := in(sc, "Enter block file path (default: chain.block): ")
	if blockFile == "" {
		blockFile = "chain.block"
	}

	dbPath := in(sc, "Enter world state directory (default: worldstate): ")
	if dbPath == "" {
		dbPath = "worldstate"
	}

	// PostgreSQL: fixed default for docker-compose (postgres service in repo root).
	// Override with POSTGRES_URL if needed.
	dbConnStr := strings.TrimSpace(os.Getenv("POSTGRES_URL"))
	if dbConnStr == "" {
		dbConnStr = "postgres://fabric:fabric123@localhost:5432/blockchain?sslmode=disable"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	privHex, pubHex, err := loadOrGenerateEndorsementKey()
	if err != nil {
		fmt.Printf("Error: endorsement key: %v\n", err)
		return
	}
	fmt.Printf("🔐 Endorser public key: %s...\n", short(pubHex, 24))

	blockStore, err := storage.NewBlockStorage(blockFile)
	if err != nil {
		fmt.Printf("Error opening block storage: %v\n", err)
		return
	}

	worldState, err := storage.NewWorldState(dbPath)
	if err != nil {
		fmt.Printf("Error opening world state: %v\n", err)
		return
	}

	// Initialize PostgreSQL connection
	db, err := storage.NewPostgresDB(dbConnStr)
	if err != nil {
		fmt.Printf("Error connecting to PostgreSQL: %v\n", err)
		return
	}
	defer db.Close()

	deliverClient, err := deliver.NewClient(ctx)
	if err != nil {
		fmt.Printf("Error creating deliver client: %v\n", err)
		return
	}
	deliverClient.RegisterTxSignHandler(privHex, pubHex)

	// Comma-separated trusted endorser pubkeys (hex). If unset, defaults to this peer's key.
	trusted := strings.TrimSpace(os.Getenv("TRUSTED_ENDORSER_PUBLIC_KEYS"))
	if trusted == "" {
		trusted = pubHex
	}
	validator := validation.NewEngine(trusted)
	peer := peerpkg.New(deliverClient, validator, blockStore, worldState, db)
	peer.RegisterSyncHandler()

	// Deliver is 1-based: avoid replaying blocks already in chain.block (would
	// reject genesis again because local tip is non-zero).
	deliverFrom := int64(1)
	nLocal := blockStore.CommittedBlockCount()
	if nLocal > 0 {
		deliverFrom = nLocal + 1
	}
	fmt.Printf("Local chain: %d block(s) on disk → deliver from_index=%d\n", nLocal, deliverFrom)

	if err := peer.Start(ctx, ordererAddr, deliverFrom); err != nil {
		fmt.Printf("Error starting peer: %v\n", err)
		return
	}

	syncAddr := deliverClient.GetAddress()

	// Send background goroutine logs to stderr so they do not overwrite the
	// current input prompt on stdout.
	log.SetOutput(os.Stderr)

	fmt.Printf("\nCommitting peer started!\n")
	fmt.Printf("Orderer   : %s\n", ordererAddr)
	fmt.Printf("BlockFile : %s\n", blockFile)
	fmt.Printf("WorldState: %s\n", dbPath)
	if strings.TrimSpace(os.Getenv("POSTGRES_URL")) != "" {
		fmt.Printf("Database  : PostgreSQL (POSTGRES_URL)\n")
	} else {
		fmt.Printf("Database  : PostgreSQL docker-compose default (fabric@localhost:5432/blockchain)\n")
	}
	fmt.Printf("Tx-sign   : libp2p %s\n", deliver.TxSignProtocolID)
	fmt.Printf("P2P Addr  : %s\n", syncAddr)
	fmt.Printf("👉 Set CoreService env: export COMMIT_PEER_P2P=%s\n", syncAddr)
	fmt.Println()

	printHelp(os.Stdout)

	for {
		fmt.Print("> ")
		if !sc.Scan() {
			break
		}
		input := strings.TrimSpace(sc.Text())
		if input == "" {
			continue
		}

		parts := strings.Fields(input)
		cmd := strings.ToLower(parts[0])

		switch cmd {

		case "status":
			cmdStatus(os.Stdout, peer, blockFile, dbPath, syncAddr, worldState)

		case "chain":
			cmdChain(os.Stdout, blockFile)

		case "block":
			if len(parts) < 2 {
				fmt.Println("Usage: block <n>  (block number, 1-based)")
				continue
			}
			n, err := strconv.Atoi(parts[1])
			if err != nil || n < 1 {
				fmt.Printf("Invalid block number: %q\n", parts[1])
				continue
			}
			cmdBlock(os.Stdout, blockFile, n)

		case "tx":
			if len(parts) < 2 {
				fmt.Println("Usage: tx <txid>")
				continue
			}
			cmdTx(os.Stdout, blockFile, parts[1])

		case "utxo":
			if len(parts) < 3 {
				fmt.Println("Usage: utxo <txid> <output_index>")
				continue
			}
			n, err := strconv.Atoi(parts[2])
			if err != nil || n < 0 {
				fmt.Printf("Invalid output index: %q\n", parts[2])
				continue
			}
			cmdUTXO(os.Stdout, worldState, parts[1], n)

		case "worldstate":
			cmdWorldState(os.Stdout, worldState)

		case "help":
			printHelp(os.Stdout)

		case "quit", "exit":
			fmt.Println("Shutting down...")
			cancel()
			peer.Stop()
			return

		default:
			fmt.Printf("Unknown command: %q  (type 'help' for available commands)\n", cmd)
		}
	}

	cancel()
	peer.Stop()
}

// ──────────────────────────────────────────────────────────────────────────────
// Command implementations
// ──────────────────────────────────────────────────────────────────────────────

func cmdStatus(out io.Writer, peer *peerpkg.CommittingPeer, blockFile, dbPath, syncAddr string, ws *storage.WorldState) {
	s := peer.GetStats()
	utxoCount, _ := ws.UTXOCount()

	lastHashStr := "(none)"
	if s.LastBlockHash != "" {
		lastHashStr = s.LastBlockHash
	}
	lastTimeStr := "(none)"
	if !s.LastBlockTime.IsZero() {
		lastTimeStr = s.LastBlockTime.Format(time.RFC3339)
	}

	fmt.Fprintf(out, "\n=== Committing Peer Status ===\n")
	fmt.Fprintf(out, "Orderer    : %s\n", s.OrdeerAddr)
	fmt.Fprintf(out, "Block file : %s\n", blockFile)
	fmt.Fprintf(out, "World state: %s\n", dbPath)
	fmt.Fprintf(out, "Sync addr  : %s\n", syncAddr)
	fmt.Fprintf(out, "\n=== Blockchain ===\n")
	fmt.Fprintf(out, "Committed blocks : %d\n", s.BlockCount)
	fmt.Fprintf(out, "Last block hash  : %s\n", lastHashStr)
	fmt.Fprintf(out, "Last block time  : %s\n", lastTimeStr)
	fmt.Fprintf(out, "Last block txs   : %d\n", s.LastBlockTxs)
	fmt.Fprintf(out, "\n=== World State ===\n")
	fmt.Fprintf(out, "Unspent outputs (UTXOs): %d\n", utxoCount)
	fmt.Fprintln(out, "==============================")
}

func cmdChain(out io.Writer, blockFile string) {
	blocks, err := storage.ReadAll(blockFile)
	if err != nil {
		fmt.Fprintf(out, "Error reading block file: %v\n", err)
		return
	}
	if len(blocks) == 0 {
		fmt.Fprintln(out, "No blocks committed yet.")
		return
	}

	fmt.Fprintf(out, "\n=== Blockchain (%d blocks) ===\n", len(blocks))
	for i, b := range blocks {
		hashHex := hex.EncodeToString(b.Hash)
		prevHex := hex.EncodeToString(b.PrevHash)
		ts := time.Unix(b.Timestamp, 0).Format(time.RFC3339)
		fmt.Fprintf(out, "  Block #%-4d  hash=%s  prev=%s  txs=%-3d  size=%d  time=%s\n",
			i+1, short(hashHex, 16), short(prevHex, 16), len(b.Transactions), b.Size, ts)
	}
	fmt.Fprintln(out, "================================")
}

func cmdBlock(out io.Writer, blockFile string, n int) {
	blocks, err := storage.ReadAll(blockFile)
	if err != nil {
		fmt.Fprintf(out, "Error reading block file: %v\n", err)
		return
	}
	if n > len(blocks) {
		fmt.Fprintf(out, "Block #%d not found (chain has %d blocks)\n", n, len(blocks))
		return
	}
	b := blocks[n-1]
	hashHex := hex.EncodeToString(b.Hash)
	prevHex := hex.EncodeToString(b.PrevHash)
	merkleHex := hex.EncodeToString(b.MerkleRoot)
	ts := time.Unix(b.Timestamp, 0).Format(time.RFC3339)

	fmt.Fprintf(out, "\n=== Block #%d ===\n", n)
	fmt.Fprintf(out, "Hash       : %s\n", hashHex)
	fmt.Fprintf(out, "PrevHash   : %s\n", prevHex)
	fmt.Fprintf(out, "MerkleRoot : %s\n", merkleHex)
	fmt.Fprintf(out, "Timestamp  : %s\n", ts)
	fmt.Fprintf(out, "Nonce      : %d\n", b.Nonce)
	fmt.Fprintf(out, "Size       : %d bytes\n", b.Size)
	fmt.Fprintf(out, "Txs        : %d\n", len(b.Transactions))
	fmt.Fprintf(out, "\n--- Transactions ---\n")
	for j, tx := range b.Transactions {
		fmt.Fprintf(out, "  [%d] txid=%s\n", j+1, tx.Txid)
		for k, vin := range tx.Vin {
			fmt.Fprintf(out, "       in  %d: %s[%d]\n", k, short(vin.Txid, 16), vin.Vout)
		}
		for k, vout := range tx.Vout {
			addrs := strings.Join(vout.ScriptPubKey.Addresses, ", ")
			if addrs == "" {
				addrs = "(no address)"
			}
			fmt.Fprintf(out, "       out %d: value=%-10d  addr=%s\n", k, vout.Value, addrs)
		}
	}
	fmt.Fprintln(out, "================")
}

func cmdTx(out io.Writer, blockFile, txid string) {
	blocks, err := storage.ReadAll(blockFile)
	if err != nil {
		fmt.Fprintf(out, "Error reading block file: %v\n", err)
		return
	}
	for bi, b := range blocks {
		for _, tx := range b.Transactions {
			if tx.Txid != txid {
				continue
			}
			fmt.Fprintf(out, "\n=== Transaction ===\n")
			fmt.Fprintf(out, "Txid    : %s\n", tx.Txid)
			fmt.Fprintf(out, "Block   : #%d  (hash %s)\n", bi+1, short(hex.EncodeToString(b.Hash), 16))
			fmt.Fprintf(out, "Version : %d    LockTime: %d\n", tx.Version, tx.LockTime)
			fmt.Fprintf(out, "Inputs  : %d\n", len(tx.Vin))
			for i, vin := range tx.Vin {
				fmt.Fprintf(out, "  in  [%d]  prev=%s[%d]\n", i, short(vin.Txid, 16), vin.Vout)
			}
			fmt.Fprintf(out, "Outputs : %d\n", len(tx.Vout))
			for i, vout := range tx.Vout {
				addrs := strings.Join(vout.ScriptPubKey.Addresses, ", ")
				if addrs == "" {
					addrs = "(no address)"
				}
				fmt.Fprintf(out, "  out [%d]  value=%-10d  addr=%s\n", i, vout.Value, addrs)
			}
			fmt.Fprintln(out, "===================")
			return
		}
	}
	fmt.Fprintf(out, "Transaction %q not found in committed blocks.\n", txid)
}

func cmdUTXO(out io.Writer, ws *storage.WorldState, txid string, n int) {
	vout, err := ws.GetUTXO(txid, n)
	if err != nil {
		fmt.Fprintf(out, "UTXO %s[%d] not found (spent or never existed): %v\n", short(txid, 16), n, err)
		return
	}
	addrs := strings.Join(vout.ScriptPubKey.Addresses, ", ")
	if addrs == "" {
		addrs = "(no address)"
	}
	fmt.Fprintf(out, "\n=== UTXO %s[%d] ===\n", txid, n)
	fmt.Fprintf(out, "Value  : %d\n", vout.Value)
	fmt.Fprintf(out, "Index  : %d\n", vout.N)
	fmt.Fprintf(out, "Addr   : %s\n", addrs)
	fmt.Fprintf(out, "Script : %s\n", vout.ScriptPubKey.ASM)
	fmt.Fprintln(out, "===================")
}

func cmdWorldState(out io.Writer, ws *storage.WorldState) {
	entries, err := ws.AllUTXOs()
	if err != nil {
		fmt.Fprintf(out, "Error reading world state: %v\n", err)
		return
	}
	if len(entries) == 0 {
		fmt.Fprintln(out, "World state is empty (no unspent outputs).")
		return
	}
	fmt.Fprintf(out, "\n=== World State (%d UTXOs) ===\n", len(entries))
	for i, e := range entries {
		addrs := strings.Join(e.Out.ScriptPubKey.Addresses, ", ")
		if addrs == "" {
			addrs = "(no address)"
		}
		fmt.Fprintf(out, "  %4d. %s[%d]  value=%-10d  addr=%s\n",
			i+1, short(e.Txid, 16), e.Index, e.Out.Value, addrs)
	}
	fmt.Fprintln(out, "==============================")
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

func printHelp(out io.Writer) {
	fmt.Fprintln(out, "\n=== Commands ===")
	fmt.Fprintln(out, "  status               - Show peer status and blockchain summary")
	fmt.Fprintln(out, "  chain                - List all committed blocks")
	fmt.Fprintln(out, "  block <n>            - Show full details of block #n (1-based)")
	fmt.Fprintln(out, "  tx <txid>            - Find a transaction by txid across all blocks")
	fmt.Fprintln(out, "  utxo <txid> <n>      - Look up a single UTXO by (txid, output index)")
	fmt.Fprintln(out, "  worldstate           - List all unspent outputs (UTXO set)")
	fmt.Fprintln(out, "  help                 - Show this help message")
	fmt.Fprintln(out, "  quit                 - Exit")
	fmt.Fprintln(out)
}

func short(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
