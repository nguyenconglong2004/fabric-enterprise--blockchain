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

type commitEvent struct {
	at      time.Time
	txCount int
}

// CommitStats aggregates committed blocks (deliver stream or orchestrator WS).
type CommitStats struct {
	mu     sync.Mutex
	events []commitEvent
	nowFn  func() time.Time
}

func (s *CommitStats) now() time.Time {
	if s.nowFn != nil {
		return s.nowFn()
	}
	return time.Now().UTC()
}

func (s *CommitStats) record(txCount int, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, commitEvent{at: at, txCount: txCount})
}

// Snapshot returns totals over all recorded events.
func (s *CommitStats) Snapshot() (blocks, txs int64, first, last time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.events {
		blocks++
		txs += int64(e.txCount)
		if first.IsZero() || e.at.Before(first) {
			first = e.at
		}
		if last.IsZero() || e.at.After(last) {
			last = e.at
		}
	}
	return blocks, txs, first, last
}

// CountInWindow returns blocks and txs committed within [start, end] inclusive.
func (s *CommitStats) CountInWindow(start, end time.Time) (blocks, txs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.events {
		if e.at.Before(start) || e.at.After(end) {
			continue
		}
		blocks++
		txs += int64(e.txCount)
	}
	return blocks, txs
}

// WatchOrchestratorWS subscribes to orchestrator /ws/events (optional; orchestrator only).
func WatchOrchestratorWS(ctx context.Context, wsURL string, stats *CommitStats) error {
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
		stats.record(payload.TxCount, stats.now())
	}
}

func rate(blocks, txs int64, sec float64) (blocksPerSec, txPerSec float64) {
	if sec < 0.001 {
		sec = 0.001
	}
	return float64(blocks) / sec, float64(txs) / sec
}

// PrintSummary logs send rate and orderer block commit rates.
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
	fmt.Printf("--- Ingest (loadgen send) ---\n")
	fmt.Printf("Load window: %s → %s (%.1fs)\n", loadStart.Format(time.RFC3339), loadEnd.Format(time.RFC3339), loadSec)
	fmt.Printf("Sent:        %d  Failed: %d  Send rate: %.1f tx/s\n", sent, failed, float64(sent)/loadSec)

	if commit != nil {
		loadBlocks, loadTxs := commit.CountInWindow(loadStart, loadEnd)
		drainBlocks, drainTxs := commit.CountInWindow(loadStart, drainEnd)

		fmt.Printf("\n--- Orderer block commit (deliver) ---\n")
		if loadBlocks > 0 {
			bps, tps := rate(loadBlocks, loadTxs, loadSec)
			fmt.Printf("During load (%0.1fs):  %d blocks, %d tx  →  %.2f blocks/s, %.1f tx/s\n",
				loadSec, loadBlocks, loadTxs, bps, tps)
			fmt.Printf("Avg tx/block (load):   %.1f\n", float64(loadTxs)/float64(loadBlocks))
		} else {
			fmt.Println("During load:           (no blocks — orderer not committing or deliver not connected)")
		}

		totalSec := drainEnd.Sub(loadStart).Seconds()
		if totalSec < 0.001 {
			totalSec = 0.001
		}
		if drainBlocks > loadBlocks {
			bps, tps := rate(drainBlocks, drainTxs, totalSec)
			fmt.Printf("Load + drain (%0.1fs): %d blocks, %d tx  →  %.2f blocks/s, %.1f tx/s (sustained over full run)\n",
				totalSec, drainBlocks, drainTxs, bps, tps)
		} else if drainBlocks > 0 && loadBlocks == 0 {
			bps, tps := rate(drainBlocks, drainTxs, totalSec)
			fmt.Printf("Load + drain (%0.1fs): %d blocks, %d tx  →  %.2f blocks/s, %.1f tx/s\n",
				totalSec, drainBlocks, drainTxs, bps, tps)
		}

		_, _, first, last := commit.Snapshot()
		if !first.IsZero() && !last.IsZero() && last.After(first) {
			span := last.Sub(first).Seconds()
			allBlocks, allTxs, _, _ := commit.Snapshot()
			bps, tps := rate(allBlocks, allTxs, span)
			fmt.Printf("Peak span (first→last commit): %.2fs  →  %.2f blocks/s, %.1f tx/s\n", span, bps, tps)
		}
	} else {
		fmt.Println("\n--- Orderer block commit: disabled ---")
	}

	if !drainEnd.IsZero() && drainEnd.After(loadEnd) {
		fmt.Printf("\nDrain wait: %.1fs after load (included in load+drain commit window)\n", drainEnd.Sub(loadEnd).Seconds())
	}
	fmt.Println("================================")
}

// StartProgressLogger prints periodic send/commit counters.
func StartProgressLogger(ctx context.Context, interval time.Duration, sent, failed *int64, commit *CommitStats, loadStart time.Time) {
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
					_, loadTxs := commit.CountInWindow(loadStart, now.UTC())
					_, allTxs, _, _ := commit.Snapshot()
					line += fmt.Sprintf(" commit_tx=%d", allTxs)
					if !loadStart.IsZero() {
						sec := now.Sub(loadStart).Seconds()
						if sec > 0 {
							line += fmt.Sprintf(" commit_sustained=%.0f tx/s", float64(loadTxs)/sec)
						}
					}
					if !lastAt.IsZero() && allTxs > lastTx {
						dt := now.Sub(lastAt).Seconds()
						if dt > 0 {
							line += fmt.Sprintf(" commit_instant=%.0f tx/s", float64(allTxs-lastTx)/dt)
						}
					}
					lastTx = allTxs
					lastAt = now
				}
				log.Println(line)
			}
		}
	}()
}
