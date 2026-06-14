package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"coreservice/internal/storage"
)

func parseTimeQuery(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), true
		}
	}
	if ms, err := strconv.ParseInt(raw, 10, 64); err == nil && ms > 0 {
		if ms > 1_000_000_000_000 {
			return time.UnixMilli(ms).UTC(), true
		}
		return time.Unix(ms, 0).UTC(), true
	}
	return time.Time{}, false
}

// HandleBenchmarkMetrics returns submit + commit + E2E latency for an explicit window.
//
// GET /api/metrics/benchmark?since=...&until=...&tx_prefix=k6-
// GET /api/metrics/benchmark?lookback=90&tx_prefix=k6-
func (s *APIServer) HandleBenchmarkMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Only GET supported", http.StatusMethodNotAllowed)
		return
	}
	if s.DB == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "PostgreSQL not connected")
		return
	}

	txPrefix := r.URL.Query().Get("tx_prefix")
	since, hasSince := parseTimeQuery(r.URL.Query().Get("since"))
	until, hasUntil := parseTimeQuery(r.URL.Query().Get("until"))

	if !hasSince || !hasUntil {
		lookbackSec := 300
		if raw := r.URL.Query().Get("lookback"); raw != "" {
			var parsed int
			if _, err := fmt.Sscanf(raw, "%d", &parsed); err == nil && parsed > 0 {
				lookbackSec = parsed
			}
		}
		until = time.Now().UTC()
		since = until.Add(-time.Duration(lookbackSec) * time.Second)
	}

	metrics, err := s.DB.GetBenchmarkMetrics(since, until, txPrefix, s.CommitMetricsClient)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := map[string]interface{}{
		"status":    "success",
		"tx_prefix": metrics.TxPrefix,

		"window_start":   metrics.WindowStart.Format(time.RFC3339Nano),
		"window_end":     metrics.WindowEnd.Format(time.RFC3339Nano),
		"window_seconds": metrics.WindowSeconds,

		"submit_count":                metrics.SubmitCount,
		"submit_tx_per_sec_sustained": metrics.SubmitTxPerSecSustained,
		"submit_tx_per_sec_peak":      metrics.SubmitTxPerSecPeak,

		"commit_count":                metrics.CommitCount,
		"commit_tx_per_sec_sustained": metrics.CommitTxPerSecSustained,
		"commit_tx_per_sec_peak":      metrics.CommitTxPerSecPeak,
		"blocks_committed":            metrics.BlocksCommitted,
		"blocks_per_sec_sustained":    metrics.BlocksPerSecSustained,
		"avg_tx_per_block":            metrics.AvgTxPerBlock,

		"e2e_completed":       metrics.E2ECompleted,
		"e2e_pending":         metrics.E2EPending,
		"e2e_tx_per_sec_peak": metrics.E2ETxPerSecPeak,

		"latency_ms_avg": metrics.LatencyMsAvg,
		"latency_ms_min": metrics.LatencyMsMin,
		"latency_ms_max": metrics.LatencyMsMax,
		"latency_ms_p50": metrics.LatencyMsP50,
		"latency_ms_p95": metrics.LatencyMsP95,
		"latency_ms_p99": metrics.LatencyMsP99,

		"meets_submit_sustained_5000": metrics.MeetsSubmitSustained5000,
		"meets_commit_sustained_5000": metrics.MeetsCommitSustained5000,
		"meets_latency_p95_under_1s":  metrics.MeetsLatencyP95Under1s,
		"commit_data_source":          metrics.CommitDataSource,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// HandleE2EMetrics alias (lookback or since/until).
// GET /api/metrics/e2e?lookback=120&tx_prefix=k6-
func (s *APIServer) HandleE2EMetrics(w http.ResponseWriter, r *http.Request) {
	s.HandleBenchmarkMetrics(w, r)
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": msg})
}

func (s *APIServer) InitSubmitRecorder() {
	if s.DB == nil || !storage.RecordSubmitEnabled() {
		return
	}
	s.submitRecorder = storage.NewSubmitRecorder(s.DB)
}

func (s *APIServer) CloseSubmitRecorder() {
	if s.submitRecorder != nil {
		s.submitRecorder.Close()
	}
}
