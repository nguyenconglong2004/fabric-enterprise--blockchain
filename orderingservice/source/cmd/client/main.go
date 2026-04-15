package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"raft-order-service/internal/types"
	"raft-order-service/pkg/client"
)

func printHelp(out io.Writer) {
	fmt.Fprintln(out, "\n=== Commands ===")
	fmt.Fprintln(out, "  keygen              - Generate a new Ed25519 keypair")
	fmt.Fprintln(out, "  wallet <seed_hex>   - Load keypair from existing seed hex")
	fmt.Fprintln(out, "  addr                - Show current wallet address")
	fmt.Fprintln(out, "  fund <amount>       - Create a genesis (coinbase-like) UTXO for current address")
	fmt.Fprintln(out, "  utxos               - List available UTXOs (auto-syncs from peer if registered)")
	fmt.Fprintln(out, "  sync <peer_addr>    - Register committing peer; auto-sync runs before utxos/tx")
	fmt.Fprintln(out, "  tx <to_addr> <amt>  - Create and submit a signed Ed25519 transaction (auto-syncs)")
	fmt.Fprintln(out, "  start [tps]         - Start auto-send signed transactions (default 1 TPS)")
	fmt.Fprintln(out, "  stop                - Stop auto-send")
	fmt.Fprintln(out, "  speed <tps>         - Change TPS in real-time")
	fmt.Fprintln(out, "  status              - Show auto-send statistics")
	fmt.Fprintln(out, "  help                - Show this message")
	fmt.Fprintln(out, "  quit                - Exit")
	fmt.Fprintln(out)
}

// autoSync silently merges blockchain-confirmed UTXOs into the wallet.
// It is a no-op if no peer address has been registered via the sync command.
func autoSync(ctx context.Context, oc *client.OrderClient, w *walletState) {
	if w == nil || peerAddr == "" {
		return
	}
	synced, err := oc.SyncUTXOs(ctx, peerAddr, w.address)
	if err != nil {
		return
	}
	for _, u := range synced {
		key := fmt.Sprintf("%s:%d", u.Txid, u.VoutIdx)
		w.utxos[key] = u
	}
}

// peerAddr is set once by the 'sync' command and reused for every subsequent auto-sync.
var peerAddr string

// walletState holds the active keypair and local UTXO tracker.
type walletState struct {
	priv    ed25519.PrivateKey
	pub     ed25519.PublicKey
	address string
	utxos   map[string]types.ClientUTXO // key: "txid:voutIdx"
}

func newWallet(priv ed25519.PrivateKey, pub ed25519.PublicKey) *walletState {
	return &walletState{
		priv:    priv,
		pub:     pub,
		address: types.AddressFromPub(pub),
		utxos:   make(map[string]types.ClientUTXO),
	}
}

func (w *walletState) listUTXOs() []types.ClientUTXO {
	out := make([]types.ClientUTXO, 0, len(w.utxos))
	for _, u := range w.utxos {
		out = append(out, u)
	}
	return out
}

func (w *walletState) addUTXO(txid string, voutIdx int, vout types.VOUT) {
	key := fmt.Sprintf("%s:%d", txid, voutIdx)
	w.utxos[key] = types.ClientUTXO{Txid: txid, VoutIdx: voutIdx, Out: vout}
}

func (w *walletState) applyTx(tx types.Transaction) {
	for _, vin := range tx.Vin {
		key := fmt.Sprintf("%s:%d", vin.Txid, vin.Vout)
		delete(w.utxos, key)
	}
	myScript := types.MakeP2PKHScriptPubKey(w.address)
	for i, vout := range tx.Vout {
		if vout.ScriptPubKey.Hex == myScript.Hex {
			w.addUTXO(tx.Txid, i, vout)
		}
	}
}

func fundWallet(w *walletState, amount int64, counter int64) types.Transaction {
	myScript := types.MakeP2PKHScriptPubKey(w.address)
	tx := types.Transaction{
		Version: 1,
		Vin: []types.VIN{
			{
				Txid: fmt.Sprintf("%064x", counter),
				Vout: 0,
				ScriptSig: types.ScriptSig{
					ASM: fmt.Sprintf("coinbase-%d", counter),
					Hex: "",
				},
			},
		},
		Vout: []types.VOUT{
			{
				Value:        amount,
				N:            0,
				ScriptPubKey: myScript,
			},
		},
		LockTime: 0,
	}
	tx.Txid = tx.ComputeTxID()
	w.addUTXO(tx.Txid, 0, tx.Vout[0])
	return tx
}

var fundCounter int64

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

	// Send background goroutine logs to stderr so they don't overwrite the
	// current input prompt on stdout.
	log.SetOutput(os.Stderr)

	sc := bufio.NewScanner(os.Stdin)

	fmt.Print("Enter address of a node in the cluster (e.g., /ip4/127.0.0.1/tcp/6000/p2p/...): ")
	if !sc.Scan() {
		return
	}
	nodeAddr := strings.TrimSpace(sc.Text())
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

	fmt.Println("Discovering cluster nodes...")
	allNodes, err := orderClient.GetClusterNodes(addr.ID)
	if err != nil {
		fmt.Printf("Warning: Could not get full cluster list: %v\n", err)
		allNodes = []peer.AddrInfo{*addr}
	}
	fmt.Printf("Found %d node(s) in cluster\n", len(allNodes))

	targetNode := allNodes[0]

	printHelp(os.Stdout)

	var wallet *walletState
	var txCounter int64
	var sendCount int64
	var autoRunning bool
	var stopChan chan struct{}
	var speedChan chan float64

	makeAutoTx := func(n int64) (types.Transaction, error) {
		if wallet == nil {
			return types.Transaction{}, fmt.Errorf("no wallet loaded — run 'keygen' or 'wallet <seed>'")
		}
		if len(wallet.utxos) == 0 {
			fc := atomic.AddInt64(&fundCounter, 1)
			fundWallet(wallet, 100000, fc)
		}
		toAddr := wallet.address
		tx, err := types.CreateTransaction(wallet.priv, wallet.address, toAddr, 1, wallet.listUTXOs())
		if err != nil {
			fc := atomic.AddInt64(&fundCounter, 1)
			fundWallet(wallet, 100000, fc)
			tx, err = types.CreateTransaction(wallet.priv, wallet.address, toAddr, 1, wallet.listUTXOs())
		}
		if err != nil {
			return types.Transaction{}, err
		}
		wallet.applyTx(tx)
		return tx, nil
	}

	startAuto := func(tps float64) {
		if autoRunning {
			fmt.Println("Auto-send already running.")
			return
		}
		if wallet == nil {
			fmt.Println("No wallet loaded. Run 'keygen' or 'wallet <seed>' first.")
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

		fmt.Printf("Auto-send started at %.2f TPS (signed Ed25519 transactions).\n", tps)

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
					fmt.Printf("[Auto] Speed changed to %.2f TPS\n", newTPS)

				case <-statsTicker.C:
					sent := atomic.LoadInt64(&sendCount)
					recv := atomic.LoadInt64(&orderClient.AutoRecvCount)
					fmt.Printf("[Auto] Stats: sent=%d  acked=%d\n", sent, recv)

				case <-ticker.C:
					n := atomic.AddInt64(&txCounter, 1)
					tx, txErr := makeAutoTx(n)
					if txErr != nil {
						fmt.Printf("[Auto] Error building tx#%d: %v\n", n, txErr)
						continue
					}
					_, sendErr := orderClient.SubmitTransactionFast(tx, targetNode)
					if sendErr != nil {
						fmt.Printf("[Auto] Error sending tx#%d: %v\n", n, sendErr)
					} else {
						atomic.AddInt64(&sendCount, 1)
						fmt.Printf("[Auto] Sent tx#%d %s... (total: %d)\n",
							n, tx.Txid[:8], atomic.LoadInt64(&sendCount))
					}
				}
			}
		}()
	}

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
		command := strings.ToLower(parts[0])

		switch command {

		case "keygen":
			seed, priv, pub, kErr := types.NewEd25519Keypair()
			if kErr != nil {
				fmt.Printf("Error generating keypair: %v\n", kErr)
				continue
			}
			wallet = newWallet(priv, pub)
			fmt.Printf("New keypair generated.\n")
			fmt.Printf("  Seed (hex): %s\n", types.SeedToHex(seed))
			fmt.Printf("  Address:    %s\n", wallet.address)

		case "wallet":
			if len(parts) < 2 {
				fmt.Println("Usage: wallet <seed_hex>")
				continue
			}
			priv, kErr := types.PrivFromSeedHex(parts[1])
			if kErr != nil {
				fmt.Printf("Error loading wallet: %v\n", kErr)
				continue
			}
			pub := priv.Public().(ed25519.PublicKey)
			wallet = newWallet(priv, pub)
			fmt.Printf("Wallet loaded.\n")
			fmt.Printf("  Address: %s\n", wallet.address)

		case "addr":
			if wallet == nil {
				fmt.Println("No wallet loaded. Run 'keygen' or 'wallet <seed>'.")
				continue
			}
			fmt.Printf("Address: %s\n", wallet.address)

		case "fund":
			if wallet == nil {
				fmt.Println("No wallet loaded. Run 'keygen' or 'wallet <seed>'.")
				continue
			}
			amount := int64(100000)
			if len(parts) >= 2 {
				v, pErr := strconv.ParseInt(parts[1], 10, 64)
				if pErr != nil || v <= 0 {
					fmt.Println("Usage: fund <amount>")
					continue
				}
				amount = v
			}
			fc := atomic.AddInt64(&fundCounter, 1)
			tx := fundWallet(wallet, amount, fc)
			fmt.Printf("Genesis UTXO created (not submitted to network).\n")
			fmt.Printf("  Txid:    %s\n", tx.Txid)
			fmt.Printf("  Amount:  %d\n", amount)
			fmt.Printf("  Address: %s\n", wallet.address)

		case "utxos":
			if wallet == nil {
				fmt.Println("No wallet loaded.")
				continue
			}
			autoSync(ctx, orderClient, wallet)
			utxos := wallet.listUTXOs()
			if len(utxos) == 0 {
				fmt.Println("No UTXOs. Run 'fund <amount>' or 'sync <peer_addr>' to load UTXOs.")
				continue
			}
			var total int64
			fmt.Printf("UTXOs (%d):\n", len(utxos))
			for _, u := range utxos {
				fmt.Printf("  %s[%d]  value=%d\n", u.Txid[:16]+"...", u.VoutIdx, u.Out.Value)
				total += u.Out.Value
			}
			fmt.Printf("Total: %d\n", total)

		case "sync":
			if len(parts) < 2 {
				fmt.Println("Usage: sync <committing-peer-addr>")
				fmt.Println("  (the Sync addr printed by the committing peer on startup)")
				continue
			}
			if wallet == nil {
				fmt.Println("No wallet loaded. Run 'keygen' or 'wallet <seed>' first.")
				continue
			}
			peerAddr = parts[1]
			fmt.Printf("Syncing UTXOs for address %s...\n", wallet.address[:8]+"...")
			synced, sErr := orderClient.SyncUTXOs(ctx, peerAddr, wallet.address)
			if sErr != nil {
				fmt.Printf("Sync failed: %v\n", sErr)
				peerAddr = ""
				continue
			}
			added := 0
			for _, u := range synced {
				key := fmt.Sprintf("%s:%d", u.Txid, u.VoutIdx)
				if _, exists := wallet.utxos[key]; !exists {
					added++
				}
				wallet.utxos[key] = u
			}
			var total int64
			for _, u := range wallet.utxos {
				total += u.Out.Value
			}
			fmt.Printf("Sync complete: +%d new UTXO(s) from blockchain, wallet total = %d UTXO(s), balance = %d\n",
				added, len(wallet.utxos), total)
			fmt.Println("Peer registered — utxos/tx will auto-sync from now on.")

		case "tx":
			if len(parts) < 3 {
				fmt.Println("Usage: tx <to_addr> <amount>")
				continue
			}
			if wallet == nil {
				fmt.Println("No wallet loaded. Run 'keygen' or 'wallet <seed>'.")
				continue
			}
			toAddr := parts[1]
			amount, pErr := strconv.ParseInt(parts[2], 10, 64)
			if pErr != nil || amount <= 0 {
				fmt.Println("Invalid amount.")
				continue
			}
			autoSync(ctx, orderClient, wallet)
			signedTx, tErr := types.CreateTransaction(
				wallet.priv, wallet.address, toAddr, amount, wallet.listUTXOs(),
			)
			if tErr != nil {
				fmt.Printf("Error creating transaction: %v\n", tErr)
				continue
			}
			txID, sErr := orderClient.SubmitTransaction(signedTx, targetNode)
			if sErr != nil {
				fmt.Printf("Error submitting transaction: %v\n", sErr)
			} else {
				wallet.applyTx(signedTx)
				fmt.Printf("Transaction submitted: %s\n", txID)
				fmt.Printf("  Inputs:  %d  Outputs: %d\n", len(signedTx.Vin), len(signedTx.Vout))
			}

		case "start":
			tps := 1.0
			if len(parts) >= 2 {
				v, pErr := strconv.ParseFloat(parts[1], 64)
				if pErr != nil || v <= 0 {
					fmt.Println("Invalid TPS. Usage: start [tps]")
					continue
				}
				tps = v
			}
			autoSync(ctx, orderClient, wallet)
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
			tps, pErr := strconv.ParseFloat(parts[1], 64)
			if pErr != nil || tps <= 0 {
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
			walletAddr := "(none)"
			if wallet != nil {
				walletAddr = wallet.address[:8] + "..."
			}
			fmt.Printf("Auto-send: %s | Wallet: %s | TX counter: %d | Sent: %d | Acked: %d\n",
				state,
				walletAddr,
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
			printHelp(os.Stdout)

		default:
			fmt.Printf("Unknown command: %s (type 'help')\n", command)
		}
	}
}
