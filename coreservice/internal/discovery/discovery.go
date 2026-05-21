package discovery

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"coreservice/internal/network"
)

const (
	defaultCacheTTL    = 8 * time.Second
	defaultReqTimeout  = 10 * time.Second
	defaultRefreshTick = 5 * time.Second
	lastGoodMaxAge     = 2 * time.Minute // failover dial when bootstrap is temporarily down
)

// MembershipFetcher loads cluster membership from one bootstrap orderer.
type MembershipFetcher interface {
	GetMembershipFromBootstrapPeer(bootstrap string) (*network.MembershipView, error)
}

// Client caches ordering-service membership and picks dial targets after leader failover.
type Client struct {
	fetcher    MembershipFetcher
	bootstraps []string

	mu         sync.RWMutex
	cached     *network.MembershipView
	cachedAt   time.Time
	lastGood   *network.MembershipView
	lastGoodAt time.Time
	ttl        time.Duration
}

// Option configures Client.
type Option func(*Client)

// WithCacheTTL sets how long a successful Refresh result is reused without refetching.
func WithCacheTTL(d time.Duration) Option {
	return func(c *Client) { c.ttl = d }
}

// NewClient creates a discovery client. bootstraps must contain at least one orderer multiaddr.
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
func (c *Client) Refresh(ctx context.Context) (*network.MembershipView, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, defaultReqTimeout)
	defer cancel()

	var lastErr error
	for _, bootstrap := range c.bootstraps {
		mv, err := c.fetcher.GetMembershipFromBootstrapPeer(bootstrap)
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

func (c *Client) storeMembership(mv *network.MembershipView) {
	c.mu.Lock()
	c.cached = mv
	c.cachedAt = time.Now()
	c.lastGood = mv
	c.lastGoodAt = c.cachedAt
	c.mu.Unlock()
}

func (c *Client) lastGoodSnapshot() *network.MembershipView {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.lastGood == nil || time.Since(c.lastGoodAt) > lastGoodMaxAge {
		return nil
	}
	return c.lastGood
}

// Invalidate drops the hot cache so the next Snapshot refetches (lastGood kept for failover).
func (c *Client) Invalidate() {
	c.mu.Lock()
	c.cached = nil
	c.cachedAt = time.Time{}
	c.mu.Unlock()
}

// Snapshot returns the cached membership, refreshing if missing or stale.
func (c *Client) Snapshot(ctx context.Context) (*network.MembershipView, error) {
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

// AliveMembers returns members marked alive with at least one dial address.
func AliveMembers(mv *network.MembershipView) []network.MemberInfo {
	if mv == nil {
		return nil
	}
	var out []network.MemberInfo
	for _, m := range mv.Members {
		if !m.Alive || len(m.Addresses) == 0 {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

// LeaderIsAlive reports whether leader_id refers to an alive member in the view.
func LeaderIsAlive(mv *network.MembershipView) bool {
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

// PickOrdererAddr chooses one orderer multiaddr: current leader if alive, else any alive member.
func PickOrdererAddr(mv *network.MembershipView) (string, error) {
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
		return "", fmt.Errorf("discovery: no alive orderers in membership")
	}
	return MemberMultiaddr(members[0])
}

// PickAllAliveOrdererAddrs returns dial multiaddrs for every alive member (leader first if alive).
func PickAllAliveOrdererAddrs(mv *network.MembershipView) ([]string, error) {
	members := AliveMembers(mv)
	if len(members) == 0 {
		return nil, fmt.Errorf("discovery: no alive orderers")
	}
	if LeaderIsAlive(mv) {
		sort.SliceStable(members, func(i, j int) bool {
			return members[i].ID == mv.LeaderID
		})
	}
	addrs := make([]string, 0, len(members))
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

// PickAllAliveAddrInfos returns libp2p AddrInfo for every alive member (leader first if alive).
func PickAllAliveAddrInfos(mv *network.MembershipView) ([]peer.AddrInfo, error) {
	addrs, err := PickAllAliveOrdererAddrs(mv)
	if err != nil {
		return nil, err
	}
	out := make([]peer.AddrInfo, 0, len(addrs))
	for _, a := range addrs {
		ai, err := peer.AddrInfoFromString(a)
		if err != nil {
			continue
		}
		out = append(out, *ai)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("discovery: could not parse any orderer address")
	}
	return out, nil
}

// MemberMultiaddr builds a full libp2p multiaddr from a membership entry.
func MemberMultiaddr(m network.MemberInfo) (string, error) {
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

// normalizeLeader clears leader_id when it does not match an alive member (stale after failover).
func normalizeLeader(mv *network.MembershipView) {
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
