package commitpeer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client reads commit metrics from the commit peer HTTP API (ground truth at commit time).
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.BaseURL != ""
}

type CommitWindowMetrics struct {
	CommitCount             int64
	CommitTxPerSecSustained float64
	CommitTxPerSecPeak      float64
	BlocksCommitted         int64
	BlocksPerSecSustained   float64
	AvgTxPerBlock           float64
}

type ThroughputMetrics struct {
	WindowSeconds   float64
	LookbackSeconds float64
	WindowStart     *time.Time
	WindowEnd       *time.Time
	TxCommitted     int64
	BlocksCommitted int64
	TxPerSec        float64
	BlocksPerSec    float64
}

func (c *Client) get(path string, q url.Values, dest interface{}) error {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	resp, err := c.HTTPClient.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("commit peer metrics %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return err
	}
	return nil
}

// CommitWindow fetches commit-side benchmark metrics for [start, end].
func (c *Client) CommitWindow(start, end time.Time, txPrefix string) (*CommitWindowMetrics, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("commit peer metrics client not configured")
	}
	q := url.Values{}
	q.Set("since", start.UTC().Format(time.RFC3339Nano))
	q.Set("until", end.UTC().Format(time.RFC3339Nano))
	q.Set("tx_prefix", txPrefix)

	var raw map[string]interface{}
	if err := c.get("/metrics/benchmark", q, &raw); err != nil {
		return nil, err
	}
	if raw["status"] != "success" {
		return nil, fmt.Errorf("commit peer benchmark: %v", raw["error"])
	}
	return &CommitWindowMetrics{
		CommitCount:             jsonInt64(raw, "commit_count"),
		CommitTxPerSecSustained: jsonFloat(raw, "commit_tx_per_sec_sustained"),
		CommitTxPerSecPeak:      jsonFloat(raw, "commit_tx_per_sec_peak"),
		BlocksCommitted:         jsonInt64(raw, "blocks_committed"),
		BlocksPerSecSustained:   jsonFloat(raw, "blocks_per_sec_sustained"),
		AvgTxPerBlock:           jsonFloat(raw, "avg_tx_per_block"),
	}, nil
}

// Throughput calls GET /metrics/throughput with the same query shape as Core.
func (c *Client) Throughput(q url.Values) (*ThroughputMetrics, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("commit peer metrics client not configured")
	}
	var raw map[string]interface{}
	if err := c.get("/metrics/throughput", q, &raw); err != nil {
		return nil, err
	}
	if raw["status"] != "success" {
		return nil, fmt.Errorf("commit peer throughput: %v", raw["error"])
	}
	m := &ThroughputMetrics{
		WindowSeconds:   jsonFloat(raw, "window_seconds"),
		LookbackSeconds: jsonFloat(raw, "lookback_seconds"),
		TxCommitted:     jsonInt64(raw, "tx_committed"),
		BlocksCommitted: jsonInt64(raw, "blocks_committed"),
		TxPerSec:        jsonFloat(raw, "tx_per_sec"),
		BlocksPerSec:    jsonFloat(raw, "blocks_per_sec"),
	}
	if s, ok := raw["window_start"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			t = t.UTC()
			m.WindowStart = &t
		}
	}
	if s, ok := raw["window_end"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			t = t.UTC()
			m.WindowEnd = &t
		}
	}
	return m, nil
}

const lookupBatchSize = 5000

// LookupCommits resolves committed_at for txids (batched POST).
func (c *Client) LookupCommits(txids []string) (map[string]time.Time, error) {
	out := make(map[string]time.Time, len(txids))
	if !c.Enabled() || len(txids) == 0 {
		return out, nil
	}
	for i := 0; i < len(txids); i += lookupBatchSize {
		end := i + lookupBatchSize
		if end > len(txids) {
			end = len(txids)
		}
		batch := txids[i:end]
		body, err := json.Marshal(map[string]interface{}{"txids": batch})
		if err != nil {
			return nil, err
		}
		resp, err := c.HTTPClient.Post(c.BaseURL+"/metrics/commit-lookup", "application/json", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		rawBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("commit lookup: %s", strings.TrimSpace(string(rawBody)))
		}
		var raw struct {
			Status  string            `json:"status"`
			Commits map[string]string `json:"commits"`
			Error   string            `json:"error"`
		}
		if err := json.Unmarshal(rawBody, &raw); err != nil {
			return nil, err
		}
		if raw.Status != "success" {
			return nil, fmt.Errorf("commit lookup: %s", raw.Error)
		}
		for id, ts := range raw.Commits {
			if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				out[id] = t.UTC()
			}
		}
	}
	return out, nil
}

func jsonFloat(m map[string]interface{}, key string) float64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}

func jsonInt64(m map[string]interface{}, key string) int64 {
	return int64(jsonFloat(m, key))
}
