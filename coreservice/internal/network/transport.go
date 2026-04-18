package network

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
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
}

// NewTransport creates a new transport for Core Service
func NewTransport(ctx context.Context) (*Transport, error) {
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create host: %w", err)
	}

	return &Transport{
		Host: h,
		Ctx:  ctx,
	}, nil
}

// SendEndorsement sends an endorsement transaction to an Order Service node
func (t *Transport) SendEndorsement(leaderAddr peer.AddrInfo, tx interface{}) error {
	// Connect to the leader
	if err := t.Host.Connect(t.Ctx, leaderAddr); err != nil {
		return fmt.Errorf("failed to connect to leader: %w", err)
	}

	// Open stream to endorsement protocol
	s, err := t.Host.NewStream(t.Ctx, leaderAddr.ID, protocol.ID("/raft-order-service/endorsement/1.0.0"))
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}
	defer s.Close()

	// Send transaction via JSON
	encoder := json.NewEncoder(s)
	if err := encoder.Encode(tx); err != nil {
		return fmt.Errorf("failed to encode endorsement: %w", err)
	}

	return nil
}

// Close closes the transport
func (t *Transport) Close() error {
	return t.Host.Close()
}

// ID returns the host ID
func (t *Transport) ID() peer.ID {
	return t.Host.ID()
}

// GetMembershipFromOrderService fetches membership view from Order Service HTTP API
// orderServiceAddr should be like "http://localhost:8080"
func (t *Transport) GetMembershipFromOrderService(orderServiceAddr string) (*MembershipView, error) {
	url := fmt.Sprintf("%s/api/membership", orderServiceAddr)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch membership: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("membership API returned status %d: %s", resp.StatusCode, string(body))
	}

	var membership MembershipView
	if err := json.NewDecoder(resp.Body).Decode(&membership); err != nil {
		return nil, fmt.Errorf("failed to decode membership: %w", err)
	}

	return &membership, nil
}
