package main

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chzyer/readline"
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
	fmt.Fprintln(out, "  utxos               - List available UTXOs in local wallet")
	fmt.Fprintln(out, "  tx <to_addr> <amt>  - Create and submit a signed Ed25519 transaction")
	fmt.Fprintln(out, "  start [tps]         - Start auto-send signed transactions (default 1 TPS)")
	fmt.Fprintln(out, "  stop                - Stop auto-send")
	fmt.Fprintln(out, "  speed <tps>         - Change TPS in real-time")
	fmt.Fprintln(out, "  status              - Show auto-send statistics")
	fmt.Fprintln(out, "  help                - Show this message")
	fmt.Fprintln(out, "  quit                - Exit")
	fmt.Fprintln(out)
}

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

// listUTXOs returns UTXOs as a slice for use in CreateTransaction.
func (w *walletState) listUTXOs() []types.ClientUTXO {
	out := make([]types.ClientUTXO, 0, len(w.utxos))
	for _, u := range w.utxos {
		out = append(out, u)
	}
	return out
}

// addUTXO records a new spendable output.
func (w *walletState) addUTXO(txid string, voutIdx int, vout types.VOUT) {
	key := fmt.Sprintf("%s:%d", txid, voutIdx)
	w.utxos[key] = types.ClientUTXO{Txid: txid, VoutIdx: voutIdx, Out: vout}
}

// applyTx updates the local UTXO set after a transaction is submitted:
// removes spent inputs and adds change output (if any) belonging to this wallet.
func (w *walletState) applyTx(tx types.Transaction) {
	// Remove spent inputs.
	for _, vin := range tx.Vin {
		key := fmt.Sprintf("%s:%d", vin.Txid, vin.Vout)
		delete(w.utxos, key)
	}
	// Add outputs that belong to this wallet (change).
	myScript := types.MakeP2PKHScriptPubKey(w.address)
	for i, vout := range tx.Vout {
		if vout.ScriptPubKey.Hex == myScript.Hex {
			w.addUTXO(tx.Txid, i, vout)
		}
	}
}

// fundWallet creates a coinbase-like UTXO (no previous input) for bootstrapping.
// This allows the first transaction without needing an external UTXO source.
func fundWallet(w *walletState, amount int64, counter int64) types.Transaction {
	myScript := types.MakeP2PKHScriptPubKey(w.address)

	tx := types.Transaction{
		Version: 1,
		Vin: []types.VIN{
			{
				// Coinbase: empty Txid signals no previous output.
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

	// Record this output as spendable.
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

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          "> ",
		HistoryFile:     "/tmp/raft-client-history.tmp",
		InterruptPrompt: "^C",
		EOFPrompt:       "quit",
	})
	if err != nil {
		fmt.Printf("Error creating readline: %v\n", err)
		return
	}
	defer rl.Close()

	log.SetOutput(rl.Stdout())
	out := rl.Stdout()

	fmt.Fprint(out, "Enter address of a node in the cluster (e.g., /ip4/127.0.0.1/tcp/6000/p2p/...): ")
	rl.SetPrompt("")
	nodeAddr, err := rl.Readline()
	rl.SetPrompt("> ")
	if err != nil {
		return
	}
	nodeAddr = strings.TrimSpace(nodeAddr)

	if nodeAddr == "" {
		fmt.Fprintln(out, "Node address is required")
		return
	}

	if err := orderClient.ConnectToNode(nodeAddr); err != nil {
		fmt.Fprintf(out, "Error connecting to node: %v\n", err)
		return
	}

	addr, err := peer.AddrInfoFromString(nodeAddr)
	if err != nil {
		fmt.Fprintf(out, "Error parsing node address: %v\n", err)
		return
	}

	time.Sleep(2 * time.Second)

	fmt.Fprintln(out, "Discovering cluster nodes...")
	allNodes, err := orderClient.GetClusterNodes(addr.ID)
	if err != nil {
		fmt.Fprintf(out, "Warning: Could not get full cluster list: %v\n", err)
		allNodes = []peer.AddrInfo{*addr}
	}
	fmt.Fprintf(out, "Found %d node(s) in cluster\n", len(allNodes))

	targetNode := allNodes[0]

	printHelp(out)

	var wallet *walletState
	var txCounter int64
	var sendCount int64
	var autoRunning bool
	var stopChan chan struct{}
	var speedChan chan float64

	// makeAutoTx creates a signed transaction for auto-send.
	// If the wallet runs out of UTXOs it self-funds with a coinbase-like tx first.
	makeAutoTx := func(n int64) (types.Transaction, error) {
		if wallet == nil {
			return types.Transaction{}, fmt.Errorf("no wallet loaded — run 'keygen' or 'wallet <seed>'")
		}
		// Ensure at least one UTXO exists.
		if len(wallet.utxos) == 0 {
			fc := atomic.AddInt64(&fundCounter, 1)
			fundWallet(wallet, 100000, fc)
		}
		// Send a small amount to ourselves for demo purposes.
		toAddr := wallet.address
		tx, err := types.CreateTransaction(wallet.priv, wallet.address, toAddr, 1, wallet.listUTXOs())
		if err != nil {
			// Self-fund and retry once.
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
			fmt.Fprintln(out, "Auto-send already running.")
			return
		}
		if wallet == nil {
			fmt.Fprintln(out, "No wallet loaded. Run 'keygen' or 'wallet <seed>' first.")
			return
		}
		if tps <= 0 {
			fmt.Fprintln(out, "TPS must be > 0.")
			return
		}

		autoRunning = true
		orderClient.AutoMode = true
		stopChan = make(chan struct{})
		speedChan = make(chan float64, 1)

		fmt.Fprintf(out, "Auto-send started at %.2f TPS (signed Ed25519 transactions).\n", tps)

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
					fmt.Fprintf(out, "[Auto] Speed changed to %.2f TPS\n", newTPS)

				case <-statsTicker.C:
					sent := atomic.LoadInt64(&sendCount)
					recv := atomic.LoadInt64(&orderClient.AutoRecvCount)
					fmt.Fprintf(out, "[Auto] Stats: sent=%d  acked=%d\n", sent, recv)

				case <-ticker.C:
					n := atomic.AddInt64(&txCounter, 1)
					tx, txErr := makeAutoTx(n)
					if txErr != nil {
						fmt.Fprintf(out, "[Auto] Error building tx#%d: %v\n", n, txErr)
						continue
					}
					_, sendErr := orderClient.SubmitTransactionFast(tx, targetNode)
					if sendErr != nil {
						fmt.Fprintf(out, "[Auto] Error sending tx#%d: %v\n", n, sendErr)
					} else {
						atomic.AddInt64(&sendCount, 1)
						fmt.Fprintf(out, "[Auto] Sent tx#%d %s... (total: %d)\n",
							n, tx.Txid[:8], atomic.LoadInt64(&sendCount))
					}
				}
			}
		}()
	}

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
		command := strings.ToLower(parts[0])

		switch command {

		case "keygen":
			seed, priv, pub, kErr := types.NewEd25519Keypair()
			if kErr != nil {
				fmt.Fprintf(out, "Error generating keypair: %v\n", kErr)
				continue
			}
			wallet = newWallet(priv, pub)
			fmt.Fprintf(out, "New keypair generated.\n")
			fmt.Fprintf(out, "  Seed (hex): %s\n", types.SeedToHex(seed))
			fmt.Fprintf(out, "  Address:    %s\n", wallet.address)

		case "wallet":
			if len(parts) < 2 {
				fmt.Fprintln(out, "Usage: wallet <seed_hex>")
				continue
			}
			priv, kErr := types.PrivFromSeedHex(parts[1])
			if kErr != nil {
				fmt.Fprintf(out, "Error loading wallet: %v\n", kErr)
				continue
			}
			pub := priv.Public().(ed25519.PublicKey)
			wallet = newWallet(priv, pub)
			fmt.Fprintf(out, "Wallet loaded.\n")
			fmt.Fprintf(out, "  Address: %s\n", wallet.address)

		case "addr":
			if wallet == nil {
				fmt.Fprintln(out, "No wallet loaded. Run 'keygen' or 'wallet <seed>'.")
				continue
			}
			fmt.Fprintf(out, "Address: %s\n", wallet.address)

		case "fund":
			if wallet == nil {
				fmt.Fprintln(out, "No wallet loaded. Run 'keygen' or 'wallet <seed>'.")
				continue
			}
			amount := int64(100000) // default
			if len(parts) >= 2 {
				v, pErr := strconv.ParseInt(parts[1], 10, 64)
				if pErr != nil || v <= 0 {
					fmt.Fprintln(out, "Usage: fund <amount>")
					continue
				}
				amount = v
			}
			fc := atomic.AddInt64(&fundCounter, 1)
			tx := fundWallet(wallet, amount, fc)
			fmt.Fprintf(out, "Genesis UTXO created (not submitted to network).\n")
			fmt.Fprintf(out, "  Txid:    %s\n", tx.Txid)
			fmt.Fprintf(out, "  Amount:  %d\n", amount)
			fmt.Fprintf(out, "  Address: %s\n", wallet.address)

		case "utxos":
			if wallet == nil {
				fmt.Fprintln(out, "No wallet loaded.")
				continue
			}
			utxos := wallet.listUTXOs()
			if len(utxos) == 0 {
				fmt.Fprintln(out, "No UTXOs. Run 'fund <amount>' to create a genesis UTXO.")
				continue
			}
			var total int64
			fmt.Fprintf(out, "UTXOs (%d):\n", len(utxos))
			for _, u := range utxos {
				fmt.Fprintf(out, "  %s[%d]  value=%d\n", u.Txid[:16]+"...", u.VoutIdx, u.Out.Value)
				total += u.Out.Value
			}
			fmt.Fprintf(out, "Total: %d\n", total)

		case "tx":
			if len(parts) < 3 {
				fmt.Fprintln(out, "Usage: tx <to_addr> <amount>")
				continue
			}
			if wallet == nil {
				fmt.Fprintln(out, "No wallet loaded. Run 'keygen' or 'wallet <seed>'.")
				continue
			}
			toAddr := parts[1]
			amount, pErr := strconv.ParseInt(parts[2], 10, 64)
			if pErr != nil || amount <= 0 {
				fmt.Fprintln(out, "Invalid amount.")
				continue
			}

			signedTx, tErr := types.CreateTransaction(
				wallet.priv, wallet.address, toAddr, amount, wallet.listUTXOs(),
			)
			if tErr != nil {
				fmt.Fprintf(out, "Error creating transaction: %v\n", tErr)
				continue
			}

			txID, sErr := orderClient.SubmitTransaction(signedTx, targetNode)
			if sErr != nil {
				fmt.Fprintf(out, "Error submitting transaction: %v\n", sErr)
			} else {
				wallet.applyTx(signedTx)
				fmt.Fprintf(out, "Transaction submitted: %s\n", txID)
				fmt.Fprintf(out, "  Inputs:  %d  Outputs: %d\n", len(signedTx.Vin), len(signedTx.Vout))
			}

		case "start":
			tps := 1.0
			if len(parts) >= 2 {
				v, pErr := strconv.ParseFloat(parts[1], 64)
				if pErr != nil || v <= 0 {
					fmt.Fprintln(out, "Invalid TPS. Usage: start [tps]")
					continue
				}
				tps = v
			}
			startAuto(tps)

		case "stop":
			if !autoRunning {
				fmt.Fprintln(out, "Auto-send is not running.")
				continue
			}
			close(stopChan)
			autoRunning = false
			orderClient.AutoMode = false
			fmt.Fprintf(out, "Auto-send stopped. Sent: %d  Acked: %d\n",
				atomic.LoadInt64(&sendCount),
				atomic.LoadInt64(&orderClient.AutoRecvCount))

		case "speed":
			if len(parts) < 2 {
				fmt.Fprintln(out, "Usage: speed <tps>")
				continue
			}
			tps, pErr := strconv.ParseFloat(parts[1], 64)
			if pErr != nil || tps <= 0 {
				fmt.Fprintln(out, "Invalid TPS value.")
				continue
			}
			if !autoRunning {
				fmt.Fprintln(out, "Auto-send is not running.")
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
			fmt.Fprintf(out, "Auto-send: %s | Wallet: %s | TX counter: %d | Sent: %d | Acked: %d\n",
				state,
				walletAddr,
				atomic.LoadInt64(&txCounter),
				atomic.LoadInt64(&sendCount),
				atomic.LoadInt64(&orderClient.AutoRecvCount))

		case "quit", "exit":
			if autoRunning {
				close(stopChan)
			}
			fmt.Fprintln(out, "Shutting down...")
			return

		case "help":
			printHelp(out)

		default:
			fmt.Fprintf(out, "Unknown command: %s (type 'help')\n", command)
		}
	}
}
