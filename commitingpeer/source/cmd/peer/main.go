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

	"github.com/chzyer/readline"

	"commiting-peer/internal/deliver"
	peerpkg "commiting-peer/internal/peer"
	"commiting-peer/internal/storage"
	"commiting-peer/internal/validation"
)

func main() {
	fmt.Println("=== Committing Peer ===")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter orderer address (e.g. /ip4/127.0.0.1/tcp/6000/p2p/<PeerID>): ")
	ordererAddr, _ := reader.ReadString('\n')
	ordererAddr = strings.TrimSpace(ordererAddr)
	if ordererAddr == "" {
		fmt.Println("Error: orderer address is required")
		return
	}

	fmt.Print("Enter block file path (default: chain.block): ")
	blockFile, _ := reader.ReadString('\n')
	blockFile = strings.TrimSpace(blockFile)
	if blockFile == "" {
		blockFile = "chain.block"
	}

	fmt.Print("Enter world state directory (default: worldstate): ")
	dbPath, _ := reader.ReadString('\n')
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		dbPath = "worldstate"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	deliverClient, err := deliver.NewClient(ctx)
	if err != nil {
		fmt.Printf("Error creating deliver client: %v\n", err)
		return
	}

	validator := validation.NewEngine()
	peer := peerpkg.New(deliverClient, validator, blockStore, worldState)
	if err := peer.Start(ctx, ordererAddr, 1); err != nil {
		fmt.Printf("Error starting peer: %v\n", err)
		return
	}

	fmt.Printf("\nCommitting peer started!\n")
	fmt.Printf("Orderer   : %s\n", ordererAddr)
	fmt.Printf("BlockFile : %s\n", blockFile)
	fmt.Printf("WorldState: %s\n", dbPath)
	fmt.Println()

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          "> ",
		HistoryFile:     "/tmp/commiting-peer-history.tmp",
		InterruptPrompt: "^C",
		EOFPrompt:       "quit",
	})
	if err != nil {
		fmt.Printf("Error creating readline: %v\n", err)
		return
	}
	defer rl.Close()

	log.SetOutput(rl.Stdout())

	printHelp(rl.Stdout())

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
		cmd := strings.ToLower(parts[0])
		out := rl.Stdout()

		switch cmd {

		// ─── status ───────────────────────────────────────────────────────────
		case "status":
			cmdStatus(out, peer, blockFile, dbPath, worldState)

		// ─── chain ────────────────────────────────────────────────────────────
		case "chain":
			cmdChain(out, blockFile)

		// ─── block <n> ────────────────────────────────────────────────────────
		case "block":
			if len(parts) < 2 {
				fmt.Fprintln(out, "Usage: block <n>  (block number, 1-based)")
				continue
			}
			n, err := strconv.Atoi(parts[1])
			if err != nil || n < 1 {
				fmt.Fprintf(out, "Invalid block number: %q\n", parts[1])
				continue
			}
			cmdBlock(out, blockFile, n)

		// ─── tx <txid> ────────────────────────────────────────────────────────
		case "tx":
			if len(parts) < 2 {
				fmt.Fprintln(out, "Usage: tx <txid>")
				continue
			}
			cmdTx(out, blockFile, parts[1])

		// ─── utxo <txid> <n> ─────────────────────────────────────────────────
		case "utxo":
			if len(parts) < 3 {
				fmt.Fprintln(out, "Usage: utxo <txid> <output_index>")
				continue
			}
			n, err := strconv.Atoi(parts[2])
			if err != nil || n < 0 {
				fmt.Fprintf(out, "Invalid output index: %q\n", parts[2])
				continue
			}
			cmdUTXO(out, worldState, parts[1], n)

		// ─── worldstate ───────────────────────────────────────────────────────
		case "worldstate":
			cmdWorldState(out, worldState)

		// ─── help / quit ──────────────────────────────────────────────────────
		case "help":
			printHelp(out)

		case "quit", "exit":
			fmt.Fprintln(out, "Shutting down...")
			cancel()
			peer.Stop()
			return

		default:
			fmt.Fprintf(out, "Unknown command: %q  (type 'help' for available commands)\n", cmd)
		}
	}

	cancel()
	peer.Stop()
}

// ──────────────────────────────────────────────────────────────────────────────
// Command implementations
// ──────────────────────────────────────────────────────────────────────────────

func cmdStatus(out io.Writer, peer *peerpkg.CommittingPeer, blockFile, dbPath string, ws *storage.WorldState) {
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

// short returns the first n characters of s (useful for truncating hashes).
func short(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
