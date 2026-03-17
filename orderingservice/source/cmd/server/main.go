package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chzyer/readline"

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

	// Setup readline để tách log khỏi input
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          "> ",
		HistoryFile:     "/tmp/raft-node-history.tmp",
		InterruptPrompt: "^C",
		EOFPrompt:       "quit",
	})
	if err != nil {
		fmt.Printf("Error creating readline: %v\n", err)
		return
	}
	defer rl.Close()

	// Redirect tất cả log.Printf sang readline.Stdout để không chen vào input
	log.SetOutput(rl.Stdout())

	fmt.Fprintln(rl.Stdout(), "\n=== Commands ===")
	fmt.Fprintln(rl.Stdout(), "  status              - Show node status (membership, raft log, ordering blocks)")
	fmt.Fprintln(rl.Stdout(), "  propose [n]         - Propose a block with first n tx from pool (default 3)")
	fmt.Fprintln(rl.Stdout(), "  autoblock start     - Start auto-propose blocks")
	fmt.Fprintln(rl.Stdout(), "  autoblock stop      - Stop auto-propose blocks")
	fmt.Fprintln(rl.Stdout(), "  connect <addr>      - Connect to another node")
	fmt.Fprintln(rl.Stdout(), "  quit                - Exit")
	fmt.Fprintln(rl.Stdout())

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

		parts := strings.SplitN(input, " ", 2)
		command := strings.ToLower(parts[0])

		out := rl.Stdout()
		switch command {
		case "status":
			node.PrintStatus(out)

		case "propose":
			if !node.IsLeader() {
				fmt.Fprintln(out, "Error: only leader can propose blocks")
				continue
			}
			n := raft.AutoProposeBlockSize
			if len(parts) >= 2 {
				v, err := strconv.Atoi(strings.TrimSpace(parts[1]))
				if err != nil || v < 1 {
					fmt.Fprintln(out, "Usage: propose [n]  (n = max number of tx, default 3)")
					continue
				}
				n = v
			}
			if err := node.ProposeBlock(n); err != nil {
				fmt.Fprintf(out, "Error: %v\n", err)
			} else {
				fmt.Fprintln(out, "Block proposal sent")
			}

		case "autoblock":
			if len(parts) < 2 {
				fmt.Fprintln(out, "Usage: autoblock start | autoblock stop")
				continue
			}
			sub := strings.ToLower(strings.TrimSpace(parts[1]))
			switch sub {
			case "start":
				if !node.IsLeader() {
					fmt.Fprintln(out, "Error: only leader can auto-propose blocks")
					continue
				}
				if err := node.StartAutoProposeBlock(raft.AutoProposeBlockSize); err != nil {
					fmt.Fprintf(out, "Error: %v\n", err)
				} else {
					fmt.Fprintf(out, "Auto-propose started (%d tx/block).\n", raft.AutoProposeBlockSize)
				}
			case "stop":
				if !node.IsAutoProposeRunning() {
					fmt.Fprintln(out, "Auto-propose is not running.")
					continue
				}
				node.StopAutoProposeBlock()
				fmt.Fprintln(out, "Auto-propose stopped.")
			default:
				fmt.Fprintln(out, "Usage: autoblock start | autoblock stop")
			}

		case "connect":
			if len(parts) < 2 {
				fmt.Fprintln(out, "Usage: connect <address>")
				continue
			}
			if err := node.ConnectToPeer(parts[1]); err != nil {
				fmt.Fprintf(out, "Error connecting: %v\n", err)
			} else {
				fmt.Fprintln(out, "Connected successfully")
			}

		case "quit", "exit":
			node.StopAutoProposeBlock()
			fmt.Fprintln(out, "Shutting down...")
			return

		case "help":
			fmt.Fprintln(out, "\n=== Commands ===")
			fmt.Fprintln(out, "  status              - Show node status")
			fmt.Fprintln(out, "  propose [n]         - Propose a block with first n tx from pool (default 3)")
			fmt.Fprintln(out, "  autoblock start     - Start auto-propose blocks")
			fmt.Fprintln(out, "  autoblock stop      - Stop auto-propose blocks")
			fmt.Fprintln(out, "  connect <addr>      - Connect to another node")
			fmt.Fprintln(out, "  quit                - Exit")
			fmt.Fprintln(out)

		default:
			fmt.Fprintf(out, "Unknown command: %s (type 'help' for commands)\n", command)
		}
	}
}
