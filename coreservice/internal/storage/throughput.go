package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// ThroughputMetrics counts commits in a time slice on the ledger mirror (Postgres).
type ThroughputMetrics struct {
	WindowSeconds   float64    `json:"window_seconds"`
	LookbackSeconds float64    `json:"lookback_seconds,omitempty"` // peak mode: search range
	WindowStart     *time.Time `json:"window_start,omitempty"`
	WindowEnd       *time.Time `json:"window_end,omitempty"`
	TxCommitted     int64      `json:"tx_committed"`
	BlocksCommitted int64      `json:"blocks_committed"`
	TxPerSec        float64    `json:"tx_per_sec"`
	BlocksPerSec    float64    `json:"blocks_per_sec"`
}

const ledgerCommitTime = "l.committed_at"

// GetThroughputLatest counts tx/blocks in (latest_commit - window, latest_commit],
// where latest_commit is the newest ledger commit time (optionally filtered by txid prefix).
// For window=1, tx_per_sec equals the number of txs committed in that 1-second slice.
func (p *PostgresDB) GetThroughputLatest(windowSec int, txidPrefix string) (*ThroughputMetrics, error) {
	if p == nil || p.db == nil {
		return nil, fmt.Errorf("postgres not connected")
	}
	if windowSec < 1 {
		windowSec = 1
	}

	var txCount, blockCount int64
	var latest sql.NullTime

	txQuery := fmt.Sprintf(`
		WITH filtered AS (
			SELECT lt.txid, %s AS committed_at, l.id AS block_id
			FROM commit_peer.ledger_transactions lt
			INNER JOIN commit_peer.ledger l ON l.id = lt.block_id
			WHERE ($1 = '' OR lt.txid LIKE $1 || '%%')
		),
		bounds AS (
			SELECT MAX(committed_at) AS latest FROM filtered
		)
		SELECT
			(SELECT COUNT(*)::bigint FROM filtered f, bounds b
			 WHERE b.latest IS NOT NULL
			   AND f.committed_at > b.latest - ($2::text || ' seconds')::interval
			   AND f.committed_at <= b.latest),
			(SELECT latest FROM bounds)
	`, ledgerCommitTime)

	if err := p.db.QueryRow(txQuery, txidPrefix, windowSec).Scan(&txCount, &latest); err != nil {
		return nil, fmt.Errorf("throughput latest tx: %w", err)
	}

	blockQuery := fmt.Sprintf(`
		WITH bounds AS (
			SELECT MAX(%s) AS latest
			FROM commit_peer.ledger l
			INNER JOIN commit_peer.ledger_transactions lt ON lt.block_id = l.id
			WHERE ($1 = '' OR lt.txid LIKE $1 || '%%')
		)
		SELECT COUNT(DISTINCT l.id)::bigint
		FROM commit_peer.ledger l
		INNER JOIN commit_peer.ledger_transactions lt ON lt.block_id = l.id
		CROSS JOIN bounds b
		WHERE b.latest IS NOT NULL
		  AND ($1 = '' OR lt.txid LIKE $1 || '%%')
		  AND %s > b.latest - ($2::text || ' seconds')::interval
		  AND %s <= b.latest
	`, ledgerCommitTime, ledgerCommitTime, ledgerCommitTime)

	if err := p.db.QueryRow(blockQuery, txidPrefix, windowSec).Scan(&blockCount); err != nil {
		return nil, fmt.Errorf("throughput latest blocks: %w", err)
	}

	ws := float64(windowSec)
	m := &ThroughputMetrics{
		WindowSeconds:   ws,
		TxCommitted:     txCount,
		BlocksCommitted: blockCount,
		TxPerSec:        float64(txCount) / ws,
		BlocksPerSec:    float64(blockCount) / ws,
	}

	if latest.Valid {
		end := latest.Time
		start := end.Add(-time.Duration(windowSec) * time.Second)
		m.WindowEnd = &end
		m.WindowStart = &start
	}

	return m, nil
}

// GetThroughputPeak finds the densest fixed-width bucket (default 1s) in
// (latest_commit - lookback, latest_commit] and returns that bucket's tx/s.
func (p *PostgresDB) GetThroughputPeak(lookbackSec, bucketSec int, txidPrefix string) (*ThroughputMetrics, error) {
	if p == nil || p.db == nil {
		return nil, fmt.Errorf("postgres not connected")
	}
	if lookbackSec < 1 {
		lookbackSec = 60
	}
	if bucketSec < 1 {
		bucketSec = 1
	}

	var txCount, blockCount int64
	var bucketStart sql.NullTime

	peakQuery := fmt.Sprintf(`
		WITH filtered AS (
			SELECT lt.txid, %s AS committed_at, l.id AS block_id
			FROM commit_peer.ledger_transactions lt
			INNER JOIN commit_peer.ledger l ON l.id = lt.block_id
			WHERE ($1 = '' OR lt.txid LIKE $1 || '%%')
		),
		bounds AS (
			SELECT MAX(committed_at) AS latest FROM filtered
		),
		recent AS (
			SELECT f.txid, f.committed_at, f.block_id
			FROM filtered f
			CROSS JOIN bounds b
			WHERE b.latest IS NOT NULL
			  AND f.committed_at > b.latest - ($2::text || ' seconds')::interval
			  AND f.committed_at <= b.latest
		),
		tx_buckets AS (
			SELECT
				date_trunc('second', committed_at) AS bucket_start,
				COUNT(*)::bigint AS tx_count
			FROM recent
			GROUP BY 1
		)
		SELECT tx_count, bucket_start
		FROM tx_buckets
		ORDER BY tx_count DESC, bucket_start DESC
		LIMIT 1
	`, ledgerCommitTime)

	err := p.db.QueryRow(peakQuery, txidPrefix, lookbackSec).Scan(&txCount, &bucketStart)
	if err == sql.ErrNoRows || !bucketStart.Valid {
		return &ThroughputMetrics{
			WindowSeconds:   float64(bucketSec),
			LookbackSeconds: float64(lookbackSec),
			TxPerSec:        0,
			BlocksPerSec:    0,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("throughput peak tx: %w", err)
	}

	blockQuery := fmt.Sprintf(`
		WITH filtered AS (
			SELECT lt.txid, %s AS committed_at, l.id AS block_id
			FROM commit_peer.ledger_transactions lt
			INNER JOIN commit_peer.ledger l ON l.id = lt.block_id
			WHERE ($1 = '' OR lt.txid LIKE $1 || '%%')
		),
		bounds AS (
			SELECT MAX(committed_at) AS latest FROM filtered
		),
		recent AS (
			SELECT f.committed_at, f.block_id
			FROM filtered f
			CROSS JOIN bounds b
			WHERE b.latest IS NOT NULL
			  AND f.committed_at > b.latest - ($3::text || ' seconds')::interval
			  AND f.committed_at <= b.latest
		)
		SELECT COUNT(DISTINCT block_id)::bigint
		FROM recent
		WHERE committed_at >= $2
		  AND committed_at < $2 + ($4::text || ' seconds')::interval
	`, ledgerCommitTime)

	if err := p.db.QueryRow(
		blockQuery, txidPrefix, bucketStart.Time, lookbackSec, bucketSec,
	).Scan(&blockCount); err != nil {
		return nil, fmt.Errorf("throughput peak blocks: %w", err)
	}

	bs := float64(bucketSec)
	start := bucketStart.Time.UTC()
	end := start.Add(time.Duration(bucketSec) * time.Second)

	return &ThroughputMetrics{
		WindowSeconds:   bs,
		LookbackSeconds: float64(lookbackSec),
		WindowStart:     &start,
		WindowEnd:       &end,
		TxCommitted:     txCount,
		BlocksCommitted: blockCount,
		TxPerSec:        float64(txCount) / bs,
		BlocksPerSec:    float64(blockCount) / bs,
	}, nil
}

// GetThroughputSince counts all commits with commit_time >= since (legacy / average over elapsed time).
func (p *PostgresDB) GetThroughputSince(since time.Time, txidPrefix string) (*ThroughputMetrics, error) {
	if p == nil || p.db == nil {
		return nil, fmt.Errorf("postgres not connected")
	}

	secs := time.Since(since).Seconds()
	if secs < 1 {
		secs = 1
	}

	var txCount, blocksCount int64

	txQuery := fmt.Sprintf(`
		SELECT COUNT(*)::bigint
		FROM commit_peer.ledger_transactions lt
		INNER JOIN commit_peer.ledger l ON l.id = lt.block_id
		WHERE %s >= $1
		  AND ($2 = '' OR lt.txid LIKE $2 || '%%')
	`, ledgerCommitTime)
	if err := p.db.QueryRow(txQuery, since, txidPrefix).Scan(&txCount); err != nil {
		return nil, fmt.Errorf("throughput since tx: %w", err)
	}

	blockQuery := fmt.Sprintf(`
		SELECT COUNT(*)::bigint
		FROM commit_peer.ledger l
		WHERE %s >= $1
	`, ledgerCommitTime)
	if err := p.db.QueryRow(blockQuery, since).Scan(&blocksCount); err != nil {
		return nil, fmt.Errorf("throughput since blocks: %w", err)
	}

	start := since
	return &ThroughputMetrics{
		WindowSeconds:   secs,
		WindowStart:     &start,
		WindowEnd:       ptrTime(time.Now().UTC()),
		TxCommitted:     txCount,
		BlocksCommitted: blocksCount,
		TxPerSec:        float64(txCount) / secs,
		BlocksPerSec:    float64(blocksCount) / secs,
	}, nil
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
