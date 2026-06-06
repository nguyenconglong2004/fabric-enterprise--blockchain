package storage

import (
	"database/sql"
	"fmt"
	"math"
	"time"
)

// BenchmarkMetrics aggregates submit, commit, and E2E latency for an explicit time window.
type BenchmarkMetrics struct {
	TxPrefix      string    `json:"tx_prefix,omitempty"`
	WindowStart   time.Time `json:"window_start"`
	WindowEnd     time.Time `json:"window_end"`
	WindowSeconds float64   `json:"window_seconds"`

	SubmitCount             int64   `json:"submit_count"`
	SubmitTxPerSecSustained float64 `json:"submit_tx_per_sec_sustained"`
	SubmitTxPerSecPeak      float64 `json:"submit_tx_per_sec_peak"`

	CommitCount             int64   `json:"commit_count"`
	CommitTxPerSecSustained float64 `json:"commit_tx_per_sec_sustained"`
	CommitTxPerSecPeak      float64 `json:"commit_tx_per_sec_peak"`
	BlocksCommitted         int64   `json:"blocks_committed"`
	BlocksPerSecSustained   float64 `json:"blocks_per_sec_sustained"`
	AvgTxPerBlock           float64 `json:"avg_tx_per_block"`

	E2ECompleted    int64   `json:"e2e_completed"`
	E2EPending      int64   `json:"e2e_pending"`
	E2ETxPerSecPeak float64 `json:"e2e_tx_per_sec_peak"`

	LatencyMsAvg float64 `json:"latency_ms_avg"`
	LatencyMsMin float64 `json:"latency_ms_min"`
	LatencyMsMax float64 `json:"latency_ms_max"`
	LatencyMsP50 float64 `json:"latency_ms_p50"`
	LatencyMsP95 float64 `json:"latency_ms_p95"`
	LatencyMsP99 float64 `json:"latency_ms_p99"`

	MeetsSubmitSustained5000 bool `json:"meets_submit_sustained_5000"`
	MeetsCommitSustained5000 bool `json:"meets_commit_sustained_5000"`
	MeetsLatencyP95Under1s   bool `json:"meets_latency_p95_under_1s"`
}

// GetBenchmarkMetrics computes metrics for [windowStart, windowEnd].
func (p *PostgresDB) GetBenchmarkMetrics(windowStart, windowEnd time.Time, txidPrefix string) (*BenchmarkMetrics, error) {
	if p == nil || p.db == nil {
		return nil, fmt.Errorf("postgres not connected")
	}
	windowStart = windowStart.UTC()
	windowEnd = windowEnd.UTC()
	if !windowEnd.After(windowStart) {
		return nil, fmt.Errorf("window_end must be after window_start")
	}
	secs := windowEnd.Sub(windowStart).Seconds()
	if secs < 0.001 {
		secs = 0.001
	}

	m := &BenchmarkMetrics{
		TxPrefix:      txidPrefix,
		WindowStart:   windowStart,
		WindowEnd:     windowEnd,
		WindowSeconds: secs,
	}

	if err := p.fillSubmitWindow(m, windowStart, windowEnd, txidPrefix); err != nil {
		return nil, err
	}
	if err := p.fillCommitWindow(m, windowStart, windowEnd, txidPrefix); err != nil {
		return nil, err
	}
	if err := p.fillE2EWindow(m, windowStart, windowEnd, txidPrefix); err != nil {
		return nil, err
	}

	m.MeetsSubmitSustained5000 = m.SubmitTxPerSecSustained >= 5000
	m.MeetsCommitSustained5000 = m.CommitTxPerSecSustained >= 5000
	m.MeetsLatencyP95Under1s = m.E2ECompleted > 0 && m.LatencyMsP95 < 1000

	return m, nil
}

func (p *PostgresDB) fillSubmitWindow(m *BenchmarkMetrics, start, end time.Time, prefix string) error {
	countQuery := `
		SELECT COUNT(*)::bigint
		FROM core_service.tx_submit_times s
		WHERE s.submitted_at >= $1 AND s.submitted_at <= $2
		  AND ($3 = '' OR s.txid LIKE $3 || '%')
	`
	if err := p.db.QueryRow(countQuery, start, end, prefix).Scan(&m.SubmitCount); err != nil {
		return fmt.Errorf("submit count: %w", err)
	}
	m.SubmitTxPerSecSustained = float64(m.SubmitCount) / m.WindowSeconds

	peakQuery := `
		SELECT COALESCE(MAX(c), 0)::bigint
		FROM (
			SELECT date_trunc('second', submitted_at) AS bucket, COUNT(*)::bigint AS c
			FROM core_service.tx_submit_times
			WHERE submitted_at >= $1 AND submitted_at <= $2
			  AND ($3 = '' OR txid LIKE $3 || '%')
			GROUP BY 1
		) t
	`
	var peak int64
	if err := p.db.QueryRow(peakQuery, start, end, prefix).Scan(&peak); err != nil {
		return fmt.Errorf("submit peak: %w", err)
	}
	m.SubmitTxPerSecPeak = float64(peak)
	return nil
}

func (p *PostgresDB) fillCommitWindow(m *BenchmarkMetrics, start, end time.Time, prefix string) error {
	txQuery := fmt.Sprintf(`
		SELECT COUNT(*)::bigint
		FROM commit_peer.ledger_transactions lt
		INNER JOIN commit_peer.ledger l ON l.id = lt.block_id
		WHERE %s >= $1 AND %s <= $2
		  AND ($3 = '' OR lt.txid LIKE $3 || '%%')
	`, ledgerCommitTime, ledgerCommitTime)
	if err := p.db.QueryRow(txQuery, start, end, prefix).Scan(&m.CommitCount); err != nil {
		return fmt.Errorf("commit count: %w", err)
	}
	m.CommitTxPerSecSustained = float64(m.CommitCount) / m.WindowSeconds

	blockQuery := fmt.Sprintf(`
		SELECT COUNT(DISTINCT l.id)::bigint
		FROM commit_peer.ledger l
		INNER JOIN commit_peer.ledger_transactions lt ON lt.block_id = l.id
		WHERE %s >= $1 AND %s <= $2
		  AND ($3 = '' OR lt.txid LIKE $3 || '%%')
	`, ledgerCommitTime, ledgerCommitTime)
	if err := p.db.QueryRow(blockQuery, start, end, prefix).Scan(&m.BlocksCommitted); err != nil {
		return fmt.Errorf("commit blocks: %w", err)
	}
	if m.BlocksCommitted > 0 {
		m.AvgTxPerBlock = float64(m.CommitCount) / float64(m.BlocksCommitted)
	}
	m.BlocksPerSecSustained = float64(m.BlocksCommitted) / m.WindowSeconds

	peakQuery := fmt.Sprintf(`
		SELECT COALESCE(MAX(c), 0)::bigint
		FROM (
			SELECT date_trunc('second', %s) AS bucket, COUNT(*)::bigint AS c
			FROM commit_peer.ledger_transactions lt
			INNER JOIN commit_peer.ledger l ON l.id = lt.block_id
			WHERE %s >= $1 AND %s <= $2
			  AND ($3 = '' OR lt.txid LIKE $3 || '%%')
			GROUP BY 1
		) t
	`, ledgerCommitTime, ledgerCommitTime, ledgerCommitTime)
	var peak int64
	if err := p.db.QueryRow(peakQuery, start, end, prefix).Scan(&peak); err != nil {
		return fmt.Errorf("commit peak: %w", err)
	}
	m.CommitTxPerSecPeak = float64(peak)
	return nil
}

func (p *PostgresDB) fillE2EWindow(m *BenchmarkMetrics, start, end time.Time, prefix string) error {
	pendingQuery := `
		SELECT COUNT(*)::bigint
		FROM core_service.tx_submit_times s
		WHERE s.submitted_at >= $1 AND s.submitted_at <= $2
		  AND ($3 = '' OR s.txid LIKE $3 || '%')
		  AND NOT EXISTS (
			SELECT 1 FROM commit_peer.ledger_transactions lt WHERE lt.txid = s.txid
		  )
	`
	if err := p.db.QueryRow(pendingQuery, start, end, prefix).Scan(&m.E2EPending); err != nil {
		return fmt.Errorf("e2e pending: %w", err)
	}

	latencyQuery := fmt.Sprintf(`
		SELECT EXTRACT(EPOCH FROM (%s - s.submitted_at)) * 1000 AS latency_ms
		FROM core_service.tx_submit_times s
		INNER JOIN commit_peer.ledger_transactions lt ON lt.txid = s.txid
		INNER JOIN commit_peer.ledger l ON l.id = lt.block_id
		WHERE s.submitted_at >= $1 AND s.submitted_at <= $2
		  AND ($3 = '' OR s.txid LIKE $3 || '%%')
		ORDER BY latency_ms
	`, ledgerCommitTime)

	rows, err := p.db.Query(latencyQuery, start, end, prefix)
	if err != nil {
		return fmt.Errorf("e2e latencies: %w", err)
	}
	defer rows.Close()

	latencies := make([]float64, 0, 256)
	for rows.Next() {
		var ms sql.NullFloat64
		if err := rows.Scan(&ms); err != nil {
			return fmt.Errorf("e2e scan: %w", err)
		}
		if ms.Valid && ms.Float64 >= 0 {
			latencies = append(latencies, ms.Float64)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	m.E2ECompleted = int64(len(latencies))
	if len(latencies) > 0 {
		var sum float64
		for _, v := range latencies {
			sum += v
		}
		m.LatencyMsMin = latencies[0]
		m.LatencyMsMax = latencies[len(latencies)-1]
		m.LatencyMsAvg = sum / float64(len(latencies))
		m.LatencyMsP50 = percentileSorted(latencies, 50)
		m.LatencyMsP95 = percentileSorted(latencies, 95)
		m.LatencyMsP99 = percentileSorted(latencies, 99)
	}

	e2ePeakQuery := fmt.Sprintf(`
		SELECT COALESCE(MAX(c), 0)::bigint
		FROM (
			SELECT date_trunc('second', %s) AS bucket, COUNT(*)::bigint AS c
			FROM core_service.tx_submit_times s
			INNER JOIN commit_peer.ledger_transactions lt ON lt.txid = s.txid
			INNER JOIN commit_peer.ledger l ON l.id = lt.block_id
			WHERE s.submitted_at >= $1 AND s.submitted_at <= $2
			  AND ($3 = '' OR s.txid LIKE $3 || '%%')
			GROUP BY 1
		) t
	`, ledgerCommitTime)
	var e2ePeak int64
	if err := p.db.QueryRow(e2ePeakQuery, start, end, prefix).Scan(&e2ePeak); err != nil {
		return fmt.Errorf("e2e peak: %w", err)
	}
	m.E2ETxPerSecPeak = float64(e2ePeak)
	return nil
}

func percentileSorted(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// GetThroughputWindow counts commits in [start, end].
func (p *PostgresDB) GetThroughputWindow(start, end time.Time, txidPrefix string) (*ThroughputMetrics, error) {
	if p == nil || p.db == nil {
		return nil, fmt.Errorf("postgres not connected")
	}
	start = start.UTC()
	end = end.UTC()
	secs := end.Sub(start).Seconds()
	if secs < 0.001 {
		secs = 0.001
	}

	var txCount, blockCount int64
	txQuery := fmt.Sprintf(`
		SELECT COUNT(*)::bigint
		FROM commit_peer.ledger_transactions lt
		INNER JOIN commit_peer.ledger l ON l.id = lt.block_id
		WHERE %s >= $1 AND %s <= $2
		  AND ($3 = '' OR lt.txid LIKE $3 || '%%')
	`, ledgerCommitTime, ledgerCommitTime)
	if err := p.db.QueryRow(txQuery, start, end, txidPrefix).Scan(&txCount); err != nil {
		return nil, fmt.Errorf("throughput window tx: %w", err)
	}

	blockQuery := fmt.Sprintf(`
		SELECT COUNT(DISTINCT l.id)::bigint
		FROM commit_peer.ledger l
		INNER JOIN commit_peer.ledger_transactions lt ON lt.block_id = l.id
		WHERE %s >= $1 AND %s <= $2
		  AND ($3 = '' OR lt.txid LIKE $3 || '%%')
	`, ledgerCommitTime, ledgerCommitTime)
	if err := p.db.QueryRow(blockQuery, start, end, txidPrefix).Scan(&blockCount); err != nil {
		return nil, fmt.Errorf("throughput window blocks: %w", err)
	}

	return &ThroughputMetrics{
		WindowSeconds:   secs,
		WindowStart:     &start,
		WindowEnd:       &end,
		TxCommitted:     txCount,
		BlocksCommitted: blockCount,
		TxPerSec:        float64(txCount) / secs,
		BlocksPerSec:    float64(blockCount) / secs,
	}, nil
}
