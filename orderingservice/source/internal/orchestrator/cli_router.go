package orchestrator

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ExecCommand executes a CLI command string against the given managed node and returns the output.
func ExecCommand(mn *ManagedNode, input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	parts := strings.SplitN(input, " ", 2)
	command := strings.ToLower(parts[0])

	var buf bytes.Buffer

	switch command {
	case "status":
		mn.Raft.PrintStatus(&buf)

	case "connect":
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			fmt.Fprintln(&buf, "Usage: connect <address>")
			break
		}
		if err := mn.Raft.ConnectToPeer(strings.TrimSpace(parts[1])); err != nil {
			fmt.Fprintf(&buf, "Error connecting: %v\n", err)
		} else {
			fmt.Fprintln(&buf, "Connected successfully")
		}

	case "delay":
		if !mn.Raft.IsLeader() {
			fmt.Fprintln(&buf, "Error: only leader can use delay")
			break
		}
		if len(parts) < 2 {
			fmt.Fprintln(&buf, "Usage: delay <seconds> <priority1> [priority2] ...")
			break
		}
		tokens := strings.Fields(parts[1])
		if len(tokens) < 2 {
			fmt.Fprintln(&buf, "Usage: delay <seconds> <priority1> [priority2] ...")
			break
		}
		delaySecs, err := strconv.Atoi(tokens[0])
		if err != nil || delaySecs <= 0 {
			fmt.Fprintf(&buf, "Invalid delay seconds: %q\n", tokens[0])
			break
		}
		priorities := make([]int, 0, len(tokens)-1)
		parseErr := false
		for _, tok := range tokens[1:] {
			p, err := strconv.Atoi(tok)
			if err != nil || p < 0 {
				fmt.Fprintf(&buf, "Invalid priority: %q\n", tok)
				parseErr = true
				break
			}
			priorities = append(priorities, p)
		}
		if !parseErr {
			mn.Raft.SetHeartbeatDelay(priorities, time.Duration(delaySecs)*time.Second)
			fmt.Fprintf(&buf, "Next heartbeat to priority %v delayed by %ds\n", priorities, delaySecs)
		}

	case "help":
		fmt.Fprintln(&buf, "Commands: status | connect <addr> | delay <secs> <p1> [p2...] | help")

	case "quit", "exit":
		fmt.Fprintln(&buf, "Use the UI to stop this node.")

	default:
		fmt.Fprintf(&buf, "Unknown command: %s (type 'help' for commands)\n", command)
	}

	return buf.String()
}
