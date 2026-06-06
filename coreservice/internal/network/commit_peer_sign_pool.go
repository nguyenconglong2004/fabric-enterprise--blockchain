package network

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"coreservice/internal/core"
)

// SignPoolEnabled returns true unless CORE_SIGN_POOL=0 (default: reuse commit-peer connection).
func SignPoolEnabled() bool {
	v := strings.TrimSpace(os.Getenv("CORE_SIGN_POOL"))
	return v != "0" && !strings.EqualFold(v, "false")
}

func signRequestTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("CORE_SIGN_TIMEOUT"))
	if raw == "" {
		return 15 * time.Second
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	return 15 * time.Second
}

// commitPeerSignPool keeps a warm libp2p connection to one commit peer.
// Each sign still opens a new stream (commit peer closes after one round-trip),
// but avoids re-dial and re-parse on every transaction.
type commitPeerSignPool struct {
	host host.Host
	ctx  context.Context

	mu        sync.Mutex
	multiaddr string
	addrInfo  peer.AddrInfo
	connected atomic.Bool
}

func newCommitPeerSignPool(h host.Host, ctx context.Context) *commitPeerSignPool {
	return &commitPeerSignPool{host: h, ctx: ctx}
}

func (p *commitPeerSignPool) Warm(multiaddr string) error {
	multiaddr = strings.TrimSpace(multiaddr)
	if multiaddr == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.setAddrLocked(multiaddr); err != nil {
		return err
	}
	return p.connectLocked()
}

func (p *commitPeerSignPool) Sign(multiaddr string, tx *core.Transaction) error {
	multiaddr = strings.TrimSpace(multiaddr)
	if multiaddr == "" {
		return fmt.Errorf("empty commit peer multiaddr")
	}

	if err := p.ensureReady(multiaddr); err != nil {
		return err
	}

	const maxAttempts = 2
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		lastErr = p.signOnce(tx)
		if lastErr == nil {
			return nil
		}
		p.invalidateConnection()
		if err := p.ensureReady(multiaddr); err != nil {
			return err
		}
	}
	return lastErr
}

func (p *commitPeerSignPool) ensureReady(multiaddr string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.multiaddr != multiaddr || p.addrInfo.ID == "" {
		if err := p.setAddrLocked(multiaddr); err != nil {
			return err
		}
	}
	if p.connected.Load() {
		return nil
	}
	return p.connectLocked()
}

func (p *commitPeerSignPool) setAddrLocked(multiaddr string) error {
	info, err := peer.AddrInfoFromString(multiaddr)
	if err != nil {
		return fmt.Errorf("parse commit peer multiaddr: %w", err)
	}
	p.multiaddr = multiaddr
	p.addrInfo = *info
	p.connected.Store(false)
	return nil
}

func (p *commitPeerSignPool) connectLocked() error {
	if err := p.host.Connect(p.ctx, p.addrInfo); err != nil {
		p.connected.Store(false)
		return fmt.Errorf("connect commit peer: %w", err)
	}
	p.connected.Store(true)
	return nil
}

func (p *commitPeerSignPool) invalidateConnection() {
	p.connected.Store(false)
}

func (p *commitPeerSignPool) signOnce(tx *core.Transaction) error {
	p.mu.Lock()
	info := p.addrInfo
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(p.ctx, signRequestTimeout())
	defer cancel()

	stream, err := p.host.NewStream(ctx, info.ID, protocol.ID(CommitPeerTxSignProtocolID))
	if err != nil {
		return fmt.Errorf("open tx-sign stream: %w", err)
	}
	defer stream.Close()

	if err := json.NewEncoder(stream).Encode(tx); err != nil {
		return fmt.Errorf("send tx for signing: %w", err)
	}

	var resp struct {
		OK    bool            `json:"ok"`
		Error string          `json:"error"`
		Tx    json.RawMessage `json:"tx"`
	}
	if err := json.NewDecoder(stream).Decode(&resp); err != nil {
		return fmt.Errorf("read sign response: %w", err)
	}
	if !resp.OK {
		if resp.Error != "" {
			return fmt.Errorf("commit peer: %s", resp.Error)
		}
		return fmt.Errorf("commit peer signing failed")
	}
	if len(resp.Tx) == 0 {
		return fmt.Errorf("commit peer returned no tx")
	}
	if err := json.Unmarshal(resp.Tx, tx); err != nil {
		return fmt.Errorf("decode signed tx: %w", err)
	}
	hasMulti := len(tx.Endorsements) > 0
	if !hasMulti && (tx.Signature == "" || tx.SenderPubKey == "") {
		return fmt.Errorf("signed tx missing endorsements or legacy signature fields")
	}
	return nil
}
