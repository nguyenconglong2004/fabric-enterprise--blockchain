package discovery

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"commiting-peer/internal/deliver"
)

const (
	defaultCacheTTL    = 8 * time.Second
	defaultReqTimeout  = 10 * time.Second
	defaultRefreshTick = 5 * time.Second
	lastGoodMaxAge     = 2 * time.Minute
)

// MembershipFetcher loads cluster membership from one bootstrap orderer.
type MembershipFetcher interface {
	FetchMembership(ctx context.Context, bootstrap string) (*deliver.MembershipView, error)
}

// Client caches ordering-service membership and picks dial targets after leader failover.
type Client struct {
	fetcher    MembershipFetcher
	bootstraps []string

	mu         sync.RWMutex
	cached     *deliver.MembershipView
	cachedAt   time.Time
	lastGood   *deliver.MembershipView
	lastGoodAt time.Time
	ttl        time.Duration
}

// Option configures Client.
type Option func(*Client)

// WithCacheTTL sets how long a successful Refresh result is reused without refetching.
func WithCacheTTL(d time.Duration) Option {
	return func(c *Client) { c.ttl = d }
}

// NewClient creates a discovery client.
func NewClient(fetcher MembershipFetcher, bootstraps []string, opts ...Option) (*Client, error) {
	bootstraps = ParseBootstraps("", strings.Join(bootstraps, ","))
	if len(bootstraps) == 0 {
		return nil, fmt.Errorf("discovery: no bootstrap orderer addresses")
	}
	c := &Client{
		fetcher:    fetcher,
		bootstraps: bootstraps,
		ttl:        defaultCacheTTL,
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// Bootstraps returns configured bootstrap multiaddrs (copy).
func (c *Client) Bootstraps() []string {
	out := make([]string, len(c.bootstraps))
	copy(out, c.bootstraps)
	return out
}

// Refresh fetches membership from bootstrap peers until one responds.
func (c *Client) Refresh(ctx context.Context) (*deliver.MembershipView, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, defaultReqTimeout)
	defer cancel()

	var lastErr error
	for _, bootstrap := range c.bootstraps {
		mv, err := c.fetcher.FetchMembership(ctx, bootstrap)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", shortAddr(bootstrap), err)
			continue
		}
		normalizeLeader(mv)
		c.storeMembership(mv)
		return mv, nil
	}
	if lastErr != nil {
		if mv := c.lastGoodSnapshot(); mv != nil {
			return mv, nil
		}
		return nil, fmt.Errorf("discovery: all bootstraps failed: %w", lastErr)
	}
	return nil, fmt.Errorf("discovery: no bootstraps configured")
}

func (c *Client) storeMembership(mv *deliver.MembershipView) {
	c.mu.Lock()
	c.cached = mv
	c.cachedAt = time.Now()
	c.lastGood = mv
	c.lastGoodAt = c.cachedAt
	c.mu.Unlock()
}

func (c *Client) lastGoodSnapshot() *deliver.MembershipView {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.lastGood == nil || time.Since(c.lastGoodAt) > lastGoodMaxAge {
		return nil
	}
	return c.lastGood
}

// Invalidate drops the hot cache (lastGood kept for failover).
func (c *Client) Invalidate() {
	c.mu.Lock()
	c.cached = nil
	c.cachedAt = time.Time{}
	c.mu.Unlock()
}

// Snapshot returns cached membership, refreshing if missing or stale.
func (c *Client) Snapshot(ctx context.Context) (*deliver.MembershipView, error) {
	c.mu.RLock()
	mv := c.cached
	age := time.Since(c.cachedAt)
	ttl := c.ttl
	c.mu.RUnlock()

	if mv != nil && age < ttl {
		return mv, nil
	}
	mv, err := c.Refresh(ctx)
	if err != nil {
		if fallback := c.lastGoodSnapshot(); fallback != nil {
			return fallback, nil
		}
		return nil, err
	}
	return mv, nil
}

// AliveMembers returns alive members with dial addresses, sorted by priority then ID.
func AliveMembers(mv *deliver.MembershipView) []deliver.MemberInfo {
	if mv == nil {
		return nil
	}
	var out []deliver.MemberInfo
	for _, m := range mv.Members {
		if !m.Alive || len(m.Addresses) == 0 {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// LeaderIsAlive reports whether leader_id refers to an alive member.
func LeaderIsAlive(mv *deliver.MembershipView) bool {
	if mv == nil || mv.LeaderID == "" {
		return false
	}
	for _, m := range mv.Members {
		if m.ID == mv.LeaderID && m.Alive && len(m.Addresses) > 0 {
			return true
		}
	}
	return false
}

// PickOrdererAddr chooses one orderer multiaddr (leader if alive, else lowest-priority alive).
func PickOrdererAddr(mv *deliver.MembershipView) (string, error) {
	if mv == nil {
		return "", fmt.Errorf("discovery: empty membership view")
	}
	if LeaderIsAlive(mv) {
		for _, m := range mv.Members {
			if m.ID == mv.LeaderID {
				return MemberMultiaddr(m)
			}
		}
	}
	members := AliveMembers(mv)
	if len(members) == 0 {
		return "", fmt.Errorf("discovery: no alive orderers")
	}
	return MemberMultiaddr(members[0])
}

// PickAllAliveOrdererAddrs returns dial multiaddrs for all alive members (leader first if alive).
func PickAllAliveOrdererAddrs(mv *deliver.MembershipView) ([]string, error) {
	members := AliveMembers(mv)
	if len(members) == 0 {
		return nil, fmt.Errorf("discovery: no alive orderers")
	}
	if LeaderIsAlive(mv) {
		sort.SliceStable(members, func(i, j int) bool {
			return members[i].ID == mv.LeaderID
		})
	}
	var addrs []string
	for _, m := range members {
		addr, err := MemberMultiaddr(m)
		if err != nil {
			continue
		}
		addrs = append(addrs, addr)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("discovery: alive members have no dial addresses")
	}
	return addrs, nil
}

// MemberMultiaddr builds a full libp2p multiaddr from a membership entry.
func MemberMultiaddr(m deliver.MemberInfo) (string, error) {
	if m.ID == "" {
		return "", fmt.Errorf("discovery: empty peer id")
	}
	for _, a := range m.Addresses {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if strings.Contains(a, "/p2p/") {
			return a, nil
		}
	}
	if len(m.Addresses) > 0 {
		return m.Addresses[0] + "/p2p/" + m.ID, nil
	}
	return "", fmt.Errorf("discovery: no addresses for peer %s", m.ID)
}

// StartRefreshLoop periodically refreshes membership in the background.
func (c *Client) StartRefreshLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultRefreshTick
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := c.Refresh(ctx); err != nil {
					log.Printf("[discovery] background refresh failed: %v", err)
				}
			}
		}
	}()
}

func normalizeLeader(mv *deliver.MembershipView) {
	if mv == nil || mv.LeaderID == "" {
		return
	}
	if !LeaderIsAlive(mv) {
		mv.LeaderID = ""
	}
}

func shortAddr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 48 {
		return s
	}
	return s[:24] + "..." + s[len(s)-16:]
}
