package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
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

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"status": "error", "error": msg})
}

// Handler returns HTTP handlers for commit-peer metrics (ground truth, no Postgres lag).
func Handler(rec *Recorder) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics/throughput", func(w http.ResponseWriter, r *http.Request) {
		handleThroughput(w, r, rec)
	})
	mux.HandleFunc("/metrics/benchmark", func(w http.ResponseWriter, r *http.Request) {
		handleBenchmark(w, r, rec)
	})
	mux.HandleFunc("/metrics/commit-lookup", func(w http.ResponseWriter, r *http.Request) {
		handleCommitLookup(w, r, rec)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

func handleThroughput(w http.ResponseWriter, r *http.Request, rec *Recorder) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "only GET supported")
		return
	}
	if rec == nil || !rec.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "commit metrics recorder disabled (COMMIT_PEER_RECORD_METRICS=0)")
		return
	}

	txPrefix := r.URL.Query().Get("tx_prefix")
	windowSec := 1
	if raw := r.URL.Query().Get("window"); raw != "" {
		var parsed int
		if _, err := fmt.Sscanf(raw, "%d", &parsed); err == nil && parsed > 0 {
			windowSec = parsed
		}
	}

	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	var result *ThroughputResult
	var err error

	switch {
	case mode == "window":
		since, okSince := parseTimeQuery(r.URL.Query().Get("since"))
		until, okUntil := parseTimeQuery(r.URL.Query().Get("until"))
		if !okSince || !okUntil {
			writeErr(w, http.StatusBadRequest, "mode=window requires since and until (RFC3339)")
			return
		}
		result, err = rec.ThroughputWindow(since, until, txPrefix)
	case mode == "peak":
		lookbackSec := 60
		if raw := r.URL.Query().Get("lookback"); raw != "" {
			var parsed int
			if _, parseErr := fmt.Sscanf(raw, "%d", &parsed); parseErr == nil && parsed > 0 {
				lookbackSec = parsed
			}
		}
		result, err = rec.ThroughputPeak(lookbackSec, windowSec, txPrefix)
	case mode == "since" || r.URL.Query().Get("since") != "":
		mode = "since"
		since := time.Now().Add(-time.Duration(windowSec) * time.Second)
		if t, ok := parseTimeQuery(r.URL.Query().Get("since")); ok {
			since = t
		}
		result, err = rec.ThroughputSince(since, txPrefix)
	default:
		mode = "latest"
		result, err = rec.ThroughputLatest(windowSec, txPrefix)
	}

	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := map[string]interface{}{
		"status":           "success",
		"source":           "commit_peer",
		"mode":               mode,
		"tx_prefix":          txPrefix,
		"window_seconds":     result.WindowSeconds,
		"tx_committed":       result.TxCommitted,
		"blocks_committed":   result.BlocksCommitted,
		"tx_per_sec":         result.TxPerSec,
		"blocks_per_sec":     result.BlocksPerSec,
	}
	if result.WindowStart != nil {
		resp["window_start"] = result.WindowStart.UTC().Format(time.RFC3339Nano)
	}
	if result.WindowEnd != nil {
		resp["window_end"] = result.WindowEnd.UTC().Format(time.RFC3339Nano)
	}
	if result.LookbackSeconds > 0 {
		resp["lookback_seconds"] = result.LookbackSeconds
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleBenchmark(w http.ResponseWriter, r *http.Request, rec *Recorder) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "only GET supported")
		return
	}
	if rec == nil || !rec.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "commit metrics recorder disabled")
		return
	}

	txPrefix := r.URL.Query().Get("tx_prefix")
	since, hasSince := parseTimeQuery(r.URL.Query().Get("since"))
	until, hasUntil := parseTimeQuery(r.URL.Query().Get("until"))
	if !hasSince || !hasUntil {
		writeErr(w, http.StatusBadRequest, "since and until required (RFC3339)")
		return
	}

	m, err := rec.CommitWindow(since, until, txPrefix)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":                      "success",
		"source":                      "commit_peer",
		"tx_prefix":                   m.TxPrefix,
		"window_start":                m.WindowStart.Format(time.RFC3339Nano),
		"window_end":                  m.WindowEnd.Format(time.RFC3339Nano),
		"window_seconds":              m.WindowSeconds,
		"commit_count":                m.CommitCount,
		"commit_tx_per_sec_sustained": m.CommitTxPerSecSustained,
		"commit_tx_per_sec_peak":      m.CommitTxPerSecPeak,
		"blocks_committed":            m.BlocksCommitted,
		"blocks_per_sec_sustained":    m.BlocksPerSecSustained,
		"avg_tx_per_block":            m.AvgTxPerBlock,
	})
}

type commitLookupRequest struct {
	Txids []string `json:"txids"`
}

func handleCommitLookup(w http.ResponseWriter, r *http.Request, rec *Recorder) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "only POST supported")
		return
	}
	if rec == nil || !rec.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "commit metrics recorder disabled")
		return
	}

	var req commitLookupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	found := rec.Lookup(req.Txids)
	commits := make(map[string]string, len(found))
	for id, at := range found {
		commits[id] = at.UTC().Format(time.RFC3339Nano)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "success",
		"source":  "commit_peer",
		"found":   len(commits),
		"commits": commits,
	})
}

// StartHTTPServer listens on addr (e.g. :8081). Returns the server; caller shuts down via ctx.
func StartHTTPServer(ctx context.Context, addr string, rec *Recorder) *http.Server {
	if addr == "" {
		addr = ":8081"
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           Handler(rec),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      120 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[metrics] HTTP server error: %v\n", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	return srv
}
