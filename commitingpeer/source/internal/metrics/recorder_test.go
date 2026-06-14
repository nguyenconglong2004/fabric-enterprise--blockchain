package metrics

import (
	"fmt"
	"testing"
	"time"
)

func TestRecorderCommitWindowAndE2E(t *testing.T) {
	r := NewRecorder()
	start := time.Date(2026, 6, 14, 8, 0, 0, 0, time.UTC)
	mid := start.Add(30 * time.Second)
	end := start.Add(60 * time.Second)

	r.RecordBlock("aaa", []string{"k6-a", "k6-b"}, mid)
	r.RecordBlock("bbb", []string{"k6-c"}, end.Add(5*time.Second))

	cm, err := r.CommitWindow(start, end, "k6-")
	if err != nil {
		t.Fatal(err)
	}
	if cm.CommitCount != 2 {
		t.Fatalf("commit count = %d, want 2", cm.CommitCount)
	}
	if cm.BlocksCommitted != 1 {
		t.Fatalf("blocks = %d, want 1", cm.BlocksCommitted)
	}

	e2e := r.ComputeE2E([]SubmitSample{
		{Txid: "k6-a", SubmittedAt: start.Add(10 * time.Second)},
		{Txid: "k6-b", SubmittedAt: start.Add(11 * time.Second)},
		{Txid: "k6-c", SubmittedAt: start.Add(50 * time.Second)},
		{Txid: "k6-missing", SubmittedAt: start.Add(55 * time.Second)},
	})
	if e2e.Completed != 3 {
		t.Fatalf("e2e completed = %d, want 3", e2e.Completed)
	}
	if e2e.Pending != 1 {
		t.Fatalf("e2e pending = %d, want 1", e2e.Pending)
	}
	if e2e.LatencyMsP50 <= 0 {
		t.Fatalf("expected positive latency p50, got %v", e2e.LatencyMsP50)
	}
}

func TestRecorderThroughputPeak(t *testing.T) {
	r := NewRecorder()
	base := time.Date(2026, 6, 14, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 100; i++ {
		r.RecordBlock("h1", []string{fmt.Sprintf("k6-x-%d", i)}, base.Add(time.Duration(i)*time.Millisecond))
	}
	for i := 0; i < 500; i++ {
		r.RecordBlock("h2", []string{fmt.Sprintf("k6-y-%d", i)}, base.Add(1*time.Second+time.Duration(i)*time.Millisecond))
	}

	peak, err := r.ThroughputPeak(10, 1, "k6-")
	if err != nil {
		t.Fatal(err)
	}
	if peak.TxCommitted < 500 {
		t.Fatalf("peak tx = %d, want >= 500", peak.TxCommitted)
	}
}
