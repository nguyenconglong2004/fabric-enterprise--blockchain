package metrics

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// ThroughputResult mirrors Core /api/metrics/throughput JSON fields.
type ThroughputResult struct {
	WindowSeconds   float64
	LookbackSeconds float64
	WindowStart     *time.Time
	WindowEnd       *time.Time
	TxCommitted     int64
	BlocksCommitted int64
	TxPerSec        float64
	BlocksPerSec    float64
}

// CommitBenchmarkResult is the commit-side subset of Core benchmark metrics.
type CommitBenchmarkResult struct {
	TxPrefix                string
	WindowStart             time.Time
	WindowEnd               time.Time
	WindowSeconds           float64
	CommitCount             int64
	CommitTxPerSecSustained float64
	CommitTxPerSecPeak      float64
	BlocksCommitted         int64
	BlocksPerSecSustained   float64
	AvgTxPerBlock           float64
}

type txSample struct {
	txid string
	at   time.Time
}

func (r *Recorder) collectTxSamples(prefix string) []txSample {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]txSample, 0, len(r.txs))
	for id, rec := range r.txs {
		if matchesPrefix(id, prefix) {
			out = append(out, txSample{txid: id, at: rec.at})
		}
	}
	return out
}

func (r *Recorder) collectBlocks(prefix string) []blockCommit {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if prefix == "" {
		out := make([]blockCommit, len(r.blocks))
		copy(out, r.blocks)
		return out
	}
	out := make([]blockCommit, 0, len(r.blocks))
	for _, blk := range r.blocks {
		if blockMatchesPrefix(blk, prefix) {
			out = append(out, blk)
		}
	}
	return out
}

func blockMatchesPrefix(b blockCommit, prefix string) bool {
	if prefix == "" {
		return true
	}
	for _, id := range b.txids {
		if matchesPrefix(id, prefix) {
			return true
		}
	}
	return false
}

func countBlockTxs(b blockCommit, prefix string) int {
	if prefix == "" {
		return len(b.txids)
	}
	n := 0
	for _, id := range b.txids {
		if matchesPrefix(id, prefix) {
			n++
		}
	}
	return n
}

func inWindow(t, start, end time.Time) bool {
	return !t.Before(start) && !t.After(end)
}

// CommitWindow computes commit metrics for committed_at in [start, end].
func (r *Recorder) CommitWindow(start, end time.Time, prefix string) (*CommitBenchmarkResult, error) {
	if r == nil || !r.enabled {
		return nil, fmt.Errorf("commit metrics recorder disabled")
	}
	start = start.UTC()
	end = end.UTC()
	if !end.After(start) {
		return nil, fmt.Errorf("window_end must be after window_start")
	}
	secs := end.Sub(start).Seconds()
	if secs < 0.001 {
		secs = 0.001
	}

	samples := r.collectTxSamples(prefix)
	var commitCount int64
	secCounts := map[int64]int64{}

	for _, s := range samples {
		if !inWindow(s.at, start, end) {
			continue
		}
		commitCount++
		sec := s.at.Unix()
		secCounts[sec]++
	}

	blocks := r.collectBlocks(prefix)
	blockIDs := map[string]struct{}{}
	var blockCount int64
	for _, blk := range blocks {
		if !inWindow(blk.at, start, end) {
			continue
		}
		if _, seen := blockIDs[blk.hash]; seen {
			continue
		}
		blockIDs[blk.hash] = struct{}{}
		blockCount++
	}

	var peak int64
	for _, c := range secCounts {
		if c > peak {
			peak = c
		}
	}

	m := &CommitBenchmarkResult{
		TxPrefix:                prefix,
		WindowStart:             start,
		WindowEnd:               end,
		WindowSeconds:           secs,
		CommitCount:             commitCount,
		CommitTxPerSecSustained: float64(commitCount) / secs,
		CommitTxPerSecPeak:      float64(peak),
		BlocksCommitted:         blockCount,
		BlocksPerSecSustained:   float64(blockCount) / secs,
	}
	if blockCount > 0 {
		m.AvgTxPerBlock = float64(commitCount) / float64(blockCount)
	}
	return m, nil
}

// ThroughputWindow counts commits with committed_at in [start, end].
func (r *Recorder) ThroughputWindow(start, end time.Time, prefix string) (*ThroughputResult, error) {
	m, err := r.CommitWindow(start, end, prefix)
	if err != nil {
		return nil, err
	}
	return &ThroughputResult{
		WindowSeconds:   m.WindowSeconds,
		WindowStart:     &m.WindowStart,
		WindowEnd:       &m.WindowEnd,
		TxCommitted:     m.CommitCount,
		BlocksCommitted: m.BlocksCommitted,
		TxPerSec:        m.CommitTxPerSecSustained,
		BlocksPerSec:    m.BlocksPerSecSustained,
	}, nil
}

// ThroughputLatest counts tx/blocks in (latest_commit - window, latest_commit].
func (r *Recorder) ThroughputLatest(windowSec int, prefix string) (*ThroughputResult, error) {
	if r == nil || !r.enabled {
		return nil, fmt.Errorf("commit metrics recorder disabled")
	}
	if windowSec < 1 {
		windowSec = 1
	}
	samples := r.collectTxSamples(prefix)
	if len(samples) == 0 {
		ws := float64(windowSec)
		return &ThroughputResult{WindowSeconds: ws}, nil
	}
	latest := samples[0].at
	for _, s := range samples[1:] {
		if s.at.After(latest) {
			latest = s.at
		}
	}
	start := latest.Add(-time.Duration(windowSec) * time.Second)
	return r.ThroughputWindow(start, latest, prefix)
}

// ThroughputPeak finds the densest 1s bucket in (latest - lookback, latest].
func (r *Recorder) ThroughputPeak(lookbackSec, bucketSec int, prefix string) (*ThroughputResult, error) {
	if r == nil || !r.enabled {
		return nil, fmt.Errorf("commit metrics recorder disabled")
	}
	if lookbackSec < 1 {
		lookbackSec = 60
	}
	if bucketSec < 1 {
		bucketSec = 1
	}
	samples := r.collectTxSamples(prefix)
	if len(samples) == 0 {
		return &ThroughputResult{
			WindowSeconds:   float64(bucketSec),
			LookbackSeconds: float64(lookbackSec),
		}, nil
	}
	latest := samples[0].at
	for _, s := range samples[1:] {
		if s.at.After(latest) {
			latest = s.at
		}
	}
	cutoff := latest.Add(-time.Duration(lookbackSec) * time.Second)

	secCounts := map[int64]int64{}
	for _, s := range samples {
		if s.at.After(cutoff) && !s.at.After(latest) {
			secCounts[s.at.Truncate(time.Second).Unix()]++
		}
	}

	var bestSec int64
	var bestCount int64
	for sec, c := range secCounts {
		if c > bestCount || (c == bestCount && sec > bestSec) {
			bestCount = c
			bestSec = sec
		}
	}
	if bestCount == 0 {
		return &ThroughputResult{
			WindowSeconds:   float64(bucketSec),
			LookbackSeconds: float64(lookbackSec),
		}, nil
	}

	bucketStart := time.Unix(bestSec, 0).UTC()
	bucketEnd := bucketStart.Add(time.Duration(bucketSec) * time.Second)

	var txCount int64
	blockSet := map[string]struct{}{}
	for _, s := range samples {
		if !s.at.Before(bucketStart) && s.at.Before(bucketEnd) {
			txCount++
		}
	}
	for _, blk := range r.collectBlocks(prefix) {
		if !blk.at.Before(bucketStart) && blk.at.Before(bucketEnd) && blockMatchesPrefix(blk, prefix) {
			blockSet[blk.hash] = struct{}{}
		}
	}

	bs := float64(bucketSec)
	return &ThroughputResult{
		WindowSeconds:   bs,
		LookbackSeconds: float64(lookbackSec),
		WindowStart:     &bucketStart,
		WindowEnd:       &bucketEnd,
		TxCommitted:     txCount,
		BlocksCommitted: int64(len(blockSet)),
		TxPerSec:        float64(txCount) / bs,
		BlocksPerSec:    float64(len(blockSet)) / bs,
	}, nil
}

// ThroughputSince counts commits with committed_at >= since.
func (r *Recorder) ThroughputSince(since time.Time, prefix string) (*ThroughputResult, error) {
	if r == nil || !r.enabled {
		return nil, fmt.Errorf("commit metrics recorder disabled")
	}
	since = since.UTC()
	until := time.Now().UTC()
	return r.ThroughputWindow(since, until, prefix)
}

// E2EResult is computed on Core by joining submits with commit lookup; exposed for direct API use.
type E2EResult struct {
	Completed    int64
	Pending      int64
	PeakPerSec   float64
	LatencyMsAvg float64
	LatencyMsMin float64
	LatencyMsMax float64
	LatencyMsP50 float64
	LatencyMsP95 float64
	LatencyMsP99 float64
}

// ComputeE2E joins submit samples with commit times from this recorder.
func (r *Recorder) ComputeE2E(submits []SubmitSample) *E2EResult {
	out := &E2EResult{}
	if len(submits) == 0 {
		return out
	}
	txids := make([]string, len(submits))
	for i, s := range submits {
		txids[i] = s.Txid
	}
	commits := r.Lookup(txids)

	latencies := make([]float64, 0, len(submits))
	secCounts := map[int64]int64{}

	for _, s := range submits {
		at, ok := commits[s.Txid]
		if !ok {
			out.Pending++
			continue
		}
		ms := at.Sub(s.SubmittedAt).Seconds() * 1000
		if ms < 0 {
			ms = 0
		}
		latencies = append(latencies, ms)
		sec := at.Truncate(time.Second).Unix()
		secCounts[sec]++
	}

	out.Completed = int64(len(latencies))
	if len(latencies) == 0 {
		return out
	}
	sort.Float64s(latencies)
	var sum float64
	for _, v := range latencies {
		sum += v
	}
	out.LatencyMsMin = latencies[0]
	out.LatencyMsMax = latencies[len(latencies)-1]
	out.LatencyMsAvg = sum / float64(len(latencies))
	out.LatencyMsP50 = percentileSorted(latencies, 50)
	out.LatencyMsP95 = percentileSorted(latencies, 95)
	out.LatencyMsP99 = percentileSorted(latencies, 99)

	for _, c := range secCounts {
		if float64(c) > out.PeakPerSec {
			out.PeakPerSec = float64(c)
		}
	}
	return out
}

// SubmitSample is one Core accept event for E2E join.
type SubmitSample struct {
	Txid        string
	SubmittedAt time.Time
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
