package storage

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type submitRow struct {
	txid string
	at   time.Time
}

// SubmitRecorder batches INSERT into tx_submit_times (enable with CORE_RECORD_SUBMIT=1).
type SubmitRecorder struct {
	db   *PostgresDB
	ch   chan submitRow
	done chan struct{}
	wg   sync.WaitGroup
}

func RecordSubmitEnabled() bool {
	v := strings.TrimSpace(os.Getenv("CORE_RECORD_SUBMIT"))
	if v == "0" || strings.EqualFold(v, "false") {
		return false
	}
	return true
}

func NewSubmitRecorder(db *PostgresDB) *SubmitRecorder {
	if db == nil || !RecordSubmitEnabled() {
		return nil
	}
	r := &SubmitRecorder{
		db:   db,
		ch:   make(chan submitRow, 65536),
		done: make(chan struct{}),
	}
	r.wg.Add(1)
	go r.loop()
	return r
}

func (r *SubmitRecorder) Record(txid string, at time.Time) {
	if r == nil || txid == "" {
		return
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	select {
	case r.ch <- submitRow{txid: txid, at: at.UTC()}:
	default:
		// Channel full under extreme load — drop rather than block submit path.
	}
}

func (r *SubmitRecorder) Close() {
	if r == nil {
		return
	}
	close(r.done)
	r.wg.Wait()
}

func (r *SubmitRecorder) loop() {
	defer r.wg.Done()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	buf := make([]submitRow, 0, 512)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		_ = r.db.recordTxSubmitTimesBatch(buf)
		buf = buf[:0]
	}

	for {
		select {
		case <-r.done:
			for {
				select {
				case row := <-r.ch:
					buf = append(buf, row)
				default:
					flush()
					return
				}
			}
		case row := <-r.ch:
			buf = append(buf, row)
			if len(buf) >= 512 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (p *PostgresDB) recordTxSubmitTimesBatch(rows []submitRow) error {
	if p == nil || p.db == nil || len(rows) == 0 {
		return nil
	}
	// Multi-row INSERT; ON CONFLICT DO NOTHING for idempotency.
	const cols = 2
	args := make([]interface{}, 0, len(rows)*cols)
	var b strings.Builder
	b.WriteString(`INSERT INTO core_service.tx_submit_times (txid, submitted_at) VALUES `)
	for i, row := range rows {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "($%d,$%d)", i*cols+1, i*cols+2)
		args = append(args, row.txid, row.at)
	}
	b.WriteString(` ON CONFLICT (txid) DO NOTHING`)
	_, err := p.db.Exec(b.String(), args...)
	if err != nil {
		return fmt.Errorf("batch record submit times: %w", err)
	}
	return nil
}
