package loadgen

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// CommitStats aggregates block-committed events from the orchestrator WebSocket.
type CommitStats struct {
	BlocksCommitted int64
	TxCommitted     int64
	FirstCommitAt   time.Time
	LastCommitAt    time.Time
	mu              sync.Mutex
}

func (s *CommitStats) record(txCount int, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.BlocksCommitted++
	s.TxCommitted += int64(txCount)
	if s.FirstCommitAt.IsZero() {
		s.FirstCommitAt = at
	}
	s.LastCommitAt = at
}

// Snapshot returns a copy of current stats.
func (s *CommitStats) Snapshot() (blocks, txs int64, first, last time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.BlocksCommitted, s.TxCommitted, s.FirstCommitAt, s.LastCommitAt
}

// WatchOrchestratorWS subscribes to orchestrator /ws/events and counts block-committed events.
// wsURL example: ws://localhost:8080/ws/events
func WatchOrchestratorWS(ctx context.Context, wsURL string, stats *CommitStats) error {
	if stats == nil {
		stats = &CommitStats{}
	}
	u, err := url.Parse(wsURL)
	if err != nil {
		return fmt.Errorf("parse ws url: %w", err)
	}
	if u.Scheme == "http" {
		u.Scheme = "ws"
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	}

	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("ws read: %w", err)
			}
		}

		var ev struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(data, &ev); err != nil {
			continue
		}
		if ev.Type != "block-committed" {
			continue
		}

		var payload struct {
			TxCount int `json:"txCount"`
		}
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			continue
		}
		stats.record(payload.TxCount, time.Now().UTC())
	}
}

// PrintSummary logs send and commit throughput.
func PrintSummary(
	label string,
	loadStart, loadEnd time.Time,
	drainEnd time.Time,
	sent, failed int64,
	commit *CommitStats,
) {
	loadSec := loadEnd.Sub(loadStart).Seconds()
	if loadSec < 0.001 {
		loadSec = 0.001
	}

	fmt.Println()
	fmt.Printf("========== %s ==========\n", label)
	fmt.Printf("Load window: %s → %s (%.1fs)\n", loadStart.Format(time.RFC3339), loadEnd.Format(time.RFC3339), loadSec)
	fmt.Printf("Sent:        %d  Failed: %d  Send rate: %.1f tx/s\n", sent, failed, float64(sent)/loadSec)

	if commit != nil {
		blocks, txs, first, last := commit.Snapshot()
		if blocks > 0 && !first.IsZero() && !last.IsZero() {
			commitSec := last.Sub(first).Seconds()
			if commitSec <= 0 {
				commitSec = 1
			}
			fmt.Printf("Blocks committed (WS): %d  (%.2f blocks/s over commit span)\n", blocks, float64(blocks)/commitSec)
			fmt.Printf("Tx committed (WS):     %d  (%.1f tx/s over commit span)\n", txs, float64(txs)/commitSec)
			if blocks > 0 {
				fmt.Printf("Avg tx/block:          %.1f\n", float64(txs)/float64(blocks))
			}
		} else {
			fmt.Println("Blocks committed (WS): (no events — is orchestrator running with --ws?)")
		}
	}

	if !drainEnd.IsZero() && drainEnd.After(loadEnd) {
		drainSec := drainEnd.Sub(loadEnd).Seconds()
		fmt.Printf("Drain wait: %.1fs after load\n", drainSec)
	}
	fmt.Println("================================")
}

// StartProgressLogger prints periodic send/commit counters.
func StartProgressLogger(ctx context.Context, interval time.Duration, sent, failed *int64, commit *CommitStats) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var lastTx int64
		var lastAt time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				s := atomic.LoadInt64(sent)
				f := atomic.LoadInt64(failed)
				line := fmt.Sprintf("[loadgen] sent=%d failed=%d", s, f)
				if commit != nil {
					_, txs, _, _ := commit.Snapshot()
					line += fmt.Sprintf(" committed_tx=%d", txs)
					if !lastAt.IsZero() && txs > lastTx {
						dt := now.Sub(lastAt).Seconds()
						if dt > 0 {
							line += fmt.Sprintf(" commit_rate=%.0f tx/s", float64(txs-lastTx)/dt)
						}
					}
					lastTx = txs
					lastAt = now
				}
				log.Println(line)
			}
		}
	}()
}
