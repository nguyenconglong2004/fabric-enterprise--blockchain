package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// E2EMetrics summarizes full-flow throughput from ledger DB (submit → ledger_committed_at).
type E2EMetrics struct {
	WindowSeconds   float64 `json:"window_seconds"`
	TxCommitted     int64   `json:"tx_committed"`
	BlocksCommitted int64   `json:"blocks_committed"`
	TxPerSec        float64 `json:"tx_per_sec"`
	BlocksPerSec    float64 `json:"blocks_per_sec"`
	TxE2EMsAvg      float64 `json:"tx_e2e_ms_avg"`
	TxE2EMsP50       float64 `json:"tx_e2e_ms_p50"`
	TxE2EMsP95       float64 `json:"tx_e2e_ms_p95"`
	TxE2EMsMax       float64 `json:"tx_e2e_ms_max"`
	TxWithoutSubmit int64   `json:"tx_without_submitted_at"`
	Source          string  `json:"source"`
}

// GetE2EMetrics aggregates commit_peer.ledger* since the given time.
// txidPrefix filters txid (e.g. "k6-"); empty = all txs with submitted_at.
func (p *PostgresDB) GetE2EMetrics(since time.Time, txidPrefix string) (*E2EMetrics, error) {
	if p == nil || p.db == nil {
		return nil, fmt.Errorf("postgres not connected")
	}

	secs := time.Since(since).Seconds()
	if secs < 1 {
		secs = 1
	}

	var txCount, blocksCount, withoutSubmit int64
	var avgMs, p50Ms, p95Ms, maxMs sql.NullFloat64

	txQuery := `
		SELECT
			COUNT(*)::bigint,
			COUNT(*) FILTER (WHERE submitted_at IS NULL)::bigint,
			AVG(EXTRACT(EPOCH FROM (ledger_committed_at - submitted_at)) * 1000),
			PERCENTILE_CONT(0.5) WITHIN GROUP (
				ORDER BY EXTRACT(EPOCH FROM (ledger_committed_at - submitted_at)) * 1000
			),
			PERCENTILE_CONT(0.95) WITHIN GROUP (
				ORDER BY EXTRACT(EPOCH FROM (ledger_committed_at - submitted_at)) * 1000
			),
			MAX(EXTRACT(EPOCH FROM (ledger_committed_at - submitted_at)) * 1000)
		FROM commit_peer.ledger_transactions
		WHERE ledger_committed_at >= $1
		  AND ($2 = '' OR txid LIKE $2 || '%')
	`
	err := p.db.QueryRow(txQuery, since, txidPrefix).Scan(
		&txCount, &withoutSubmit, &avgMs, &p50Ms, &p95Ms, &maxMs,
	)
	if err != nil {
		return nil, fmt.Errorf("e2e tx metrics: %w", err)
	}

	blockQuery := `
		SELECT COUNT(*)::bigint
		FROM commit_peer.ledger
		WHERE COALESCE(ledger_committed_at, committed_at) >= $1
	`
	if err := p.db.QueryRow(blockQuery, since).Scan(&blocksCount); err != nil {
		return nil, fmt.Errorf("e2e block metrics: %w", err)
	}

	out := &E2EMetrics{
		WindowSeconds:   secs,
		TxCommitted:     txCount,
		BlocksCommitted: blocksCount,
		TxPerSec:        float64(txCount) / secs,
		BlocksPerSec:    float64(blocksCount) / secs,
		TxWithoutSubmit: withoutSubmit,
		Source:          "commit_peer.ledger DB (ledger_committed_at = full flow end)",
	}
	if avgMs.Valid {
		out.TxE2EMsAvg = avgMs.Float64
	}
	if p50Ms.Valid {
		out.TxE2EMsP50 = p50Ms.Float64
	}
	if p95Ms.Valid {
		out.TxE2EMsP95 = p95Ms.Float64
	}
	if maxMs.Valid {
		out.TxE2EMsMax = maxMs.Float64
	}
	return out, nil
}
