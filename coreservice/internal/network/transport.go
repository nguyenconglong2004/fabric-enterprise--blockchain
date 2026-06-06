package network

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"

	"coreservice/internal/core"
)

// Protocol IDs must match orderingservice/internal/network/protocol.go
const (
	orderProtocolMain        = "/raft-order-service/1.0.0"
	orderProtocolEndorsement = "/raft-order-service/endorsement/1.0.0"
)

// CommitPeerTxSignProtocolID must match commiting-peer/internal/deliver.TxSignProtocolID.
const CommitPeerTxSignProtocolID = "/fabric-enterprise/commit-peer/tx-sign/1.0.0"

// Message type constants must match orderingservice/internal/types/message.go (iota order).
const (
	orderMsgMembershipRequest  = 6
	orderMsgMembershipResponse = 7
)

// MemberInfo represents a member in the network
type MemberInfo struct {
	ID        string   `json:"id"`
	Addresses []string `json:"addresses"`
	Alive     bool     `json:"alive"`
}

// MembershipView represents the network membership
type MembershipView struct {
	LeaderID string       `json:"leader_id"`
	Members  []MemberInfo `json:"members"`
}

// Transport handles P2P communication with Ordering Service
type Transport struct {
	Host host.Host
	Ctx  context.Context

	membershipCh chan *MembershipView
	signPool     *commitPeerSignPool
}

// NewTransport creates a new transport for Core Service
func NewTransport(ctx context.Context) (*Transport, error) {
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create host: %w", err)
	}

	t := &Transport{
		Host:         h,
		Ctx:          ctx,
		membershipCh: make(chan *MembershipView, 1),
		signPool:     newCommitPeerSignPool(h, ctx),
	}
	h.SetStreamHandler(protocol.ID(orderProtocolMain), t.handleMainProtocolStream)
	return t, nil
}

func (t *Transport) handleMainProtocolStream(s network.Stream) {
	defer s.Close()

	var msg struct {
		Type int                    `json:"Type"`
		Data map[string]interface{} `json:"Data"`
	}
	if err := json.NewDecoder(s).Decode(&msg); err != nil {
		return
	}
	if msg.Type != orderMsgMembershipResponse {
		return
	}
	mv, err := parseMembershipData(msg.Data)
	if err != nil {
		return
	}
	select {
	case t.membershipCh <- mv:
	default:
	}
}

func parseMembershipData(data map[string]interface{}) (*MembershipView, error) {
	if data == nil {
		return nil, fmt.Errorf("empty membership data")
	}
	mv := &MembershipView{}
	if lid, ok := data["leader_id"].(string); ok {
		mv.LeaderID = lid
	}
	membersRaw, ok := data["members"].([]interface{})
	if !ok {
		return mv, nil
	}
	for _, m := range membersRaw {
		mm, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		pid, _ := mm["peer_id"].(string)
		alive, _ := mm["is_alive"].(bool)
		var addrs []string
		if arr, ok := mm["addresses"].([]interface{}); ok {
			for _, a := range arr {
				if s, ok := a.(string); ok {
					addrs = append(addrs, s)
				}
			}
		}
		mv.Members = append(mv.Members, MemberInfo{
			ID:        pid,
			Addresses: addrs,
			Alive:     alive,
		})
	}
	return mv, nil
}

// GetMembershipFromBootstrapPeer fetches membership via libp2p (same protocol as order client).
func (t *Transport) GetMembershipFromBootstrapPeer(bootstrapMultiaddr string) (*MembershipView, error) {
	addrInfo, err := peer.AddrInfoFromString(bootstrapMultiaddr)
	if err != nil {
		return nil, fmt.Errorf("invalid bootstrap multiaddr: %w", err)
	}
	if err := t.Host.Connect(t.Ctx, *addrInfo); err != nil {
		return nil, fmt.Errorf("failed to connect to order peer: %w", err)
	}

	// Drop any stale membership response before a new round-trip.
	select {
	case <-t.membershipCh:
	default:
	}

	s, err := t.Host.NewStream(t.Ctx, addrInfo.ID, protocol.ID(orderProtocolMain))
	if err != nil {
		return nil, fmt.Errorf("failed to open membership stream: %w", err)
	}
	defer s.Close()

	req := map[string]interface{}{
		"Type":      orderMsgMembershipRequest,
		"Term":      int64(0),
		"SenderID":  t.Host.ID().String(),
		"Data":      nil,
		"Timestamp": time.Now(),
	}
	if err := json.NewEncoder(s).Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send membership request: %w", err)
	}

	select {
	case mv := <-t.membershipCh:
		return mv, nil
	case <-time.After(8 * time.Second):
		return nil, fmt.Errorf("timeout waiting for membership response over libp2p")
	}
}

// SendEndorsement sends an endorsement transaction to an Order Service node over libp2p.
func (t *Transport) SendEndorsement(leaderAddr peer.AddrInfo, tx interface{}) error {
	if err := t.Host.Connect(t.Ctx, leaderAddr); err != nil {
		return fmt.Errorf("failed to connect to leader: %w", err)
	}

	stream, err := t.Host.NewStream(t.Ctx, leaderAddr.ID, protocol.ID(orderProtocolEndorsement))
	if err != nil {
		return fmt.Errorf("failed to create endorsement stream: %w", err)
	}
	defer stream.Close()

	if err := json.NewEncoder(stream).Encode(tx); err != nil {
		return fmt.Errorf("failed to encode endorsement: %w", err)
	}
	return nil
}

// SignTransactionViaCommitPeer signs via commit peer (warm connection pool by default).
func (t *Transport) SignTransactionViaCommitPeer(commitPeerMultiaddr string, tx *core.Transaction) error {
	if SignPoolEnabled() && t.signPool != nil {
		return t.signPool.Sign(commitPeerMultiaddr, tx)
	}
	return t.signTransactionViaCommitPeerDirect(commitPeerMultiaddr, tx)
}

func (t *Transport) signTransactionViaCommitPeerDirect(commitPeerMultiaddr string, tx *core.Transaction) error {
	commitPeerMultiaddr = strings.TrimSpace(commitPeerMultiaddr)
	if commitPeerMultiaddr == "" {
		return fmt.Errorf("empty commit peer multiaddr")
	}
	addrInfo, err := peer.AddrInfoFromString(commitPeerMultiaddr)
	if err != nil {
		return fmt.Errorf("parse commit peer multiaddr: %w", err)
	}
	if err := t.Host.Connect(t.Ctx, *addrInfo); err != nil {
		return fmt.Errorf("connect commit peer: %w", err)
	}

	ctx, cancel := context.WithTimeout(t.Ctx, 15*time.Second)
	defer cancel()

	stream, err := t.Host.NewStream(ctx, addrInfo.ID, protocol.ID(CommitPeerTxSignProtocolID))
	if err != nil {
		return fmt.Errorf("open tx-sign stream: %w", err)
	}
	defer stream.Close()

	if err := json.NewEncoder(stream).Encode(tx); err != nil {
		return fmt.Errorf("send tx for signing: %w", err)
	}

	var resp struct {
		OK    bool           `json:"ok"`
		Error string         `json:"error"`
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

// WarmCommitPeer dials the commit peer once so the first tx-sign stream avoids cold connect.
func (t *Transport) WarmCommitPeer(commitPeerMultiaddr string) error {
	commitPeerMultiaddr = strings.TrimSpace(commitPeerMultiaddr)
	if commitPeerMultiaddr == "" {
		return nil
	}
	if SignPoolEnabled() && t.signPool != nil {
		return t.signPool.Warm(commitPeerMultiaddr)
	}
	addrInfo, err := peer.AddrInfoFromString(commitPeerMultiaddr)
	if err != nil {
		return fmt.Errorf("parse commit peer multiaddr: %w", err)
	}
	return t.Host.Connect(t.Ctx, *addrInfo)
}

// Close closes the transport
func (t *Transport) Close() error {
	return t.Host.Close()
}

// ID returns the host ID
func (t *Transport) ID() peer.ID {
	return t.Host.ID()
}

// AddrInfoFromMember builds libp2p AddrInfo from a membership entry.
func AddrInfoFromMember(m MemberInfo) (peer.AddrInfo, error) {
	if m.ID == "" {
		return peer.AddrInfo{}, fmt.Errorf("empty peer id")
	}
	pid, err := peer.Decode(m.ID)
	if err != nil {
		return peer.AddrInfo{}, fmt.Errorf("invalid peer id: %w", err)
	}
	var mas []multiaddr.Multiaddr
	for _, a := range m.Addresses {
		ma, err := multiaddr.NewMultiaddr(a)
		if err != nil {
			continue
		}
		mas = append(mas, ma)
	}
	if len(mas) == 0 {
		return peer.AddrInfo{}, fmt.Errorf("no dial addresses for peer %s", m.ID)
	}
	return peer.AddrInfo{ID: pid, Addrs: mas}, nil
}
