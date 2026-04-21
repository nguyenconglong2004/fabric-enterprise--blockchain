package network

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
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

// GetMembershipFromOrderService fetches membership via P2P protocol
// orderServiceAddr should be like "/ip4/127.0.0.1/tcp/6000/p2p/12D3Koo..."
func (t *Transport) GetMembershipFromOrderService(orderServiceAddr string) (*MembershipView, error) {
	if orderServiceAddr == "" {
		return nil, fmt.Errorf("orderServiceAddr is empty")
	}

	// Parse the multiaddr to get peer info
	maddr, err := multiaddr.NewMultiaddr(orderServiceAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse multiaddr: %w", err)
	}

	// Extract peer ID from multiaddr
	peerInfo, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return nil, fmt.Errorf("failed to extract peer info: %w", err)
	}

	fmt.Printf("[Transport] 🔄 Fetching membership from P2P: %s\n", peerInfo.ID.ShortString())

	// Connect to the peer
	if err := t.Host.Connect(t.Ctx, *peerInfo); err != nil {
		return nil, fmt.Errorf("failed to connect to Order Service: %w", err)
	}

	// Open stream to membership protocol
	s, err := t.Host.NewStream(t.Ctx, peerInfo.ID, protocol.ID("/raft-order-service/membership/1.0.0"))
	if err != nil {
		return nil, fmt.Errorf("failed to create membership stream: %w", err)
	}
	defer s.Close()

	// Decode membership response
	decoder := json.NewDecoder(s)
	var membership MembershipView
	if err := decoder.Decode(&membership); err != nil {
		return nil, fmt.Errorf("failed to decode membership: %w", err)
	}

	fmt.Printf("[Transport] ✅ Got membership: leader=%s, members=%d\n", membership.LeaderID[:8], len(membership.Members))
	return &membership, nil
}

// SendTransaction sends a transaction to Order Service via libp2p
// memberInfo includes peer ID and addresses for connection
func (t *Transport) SendTransaction(memberInfo MemberInfo, tx interface{}) error {
	peerID, err := peer.Decode(memberInfo.ID)
	if err != nil {
		return fmt.Errorf("failed to decode peer ID: %w", err)
	}

	// Parse addresses and connect
	if len(memberInfo.Addresses) > 0 {
		for _, addrStr := range memberInfo.Addresses {
			maddr, err := multiaddr.NewMultiaddr(addrStr)
			if err != nil {
				continue
			}

			peerInfo, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				continue
			}

			// Try to connect to this address
			if err := t.Host.Connect(t.Ctx, *peerInfo); err == nil {
				fmt.Printf("[Transport] 📡 Connected to %s via %s\n", peerID.ShortString(), addrStr)
				break
			}
		}
	}

	// Open stream to transaction protocol
	s, err := t.Host.NewStream(t.Ctx, peerID, protocol.ID("/raft-order-service/transaction/1.0.0"))
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}
	defer s.Close()

	// Send transaction via JSON
	encoder := json.NewEncoder(s)
	if err := encoder.Encode(tx); err != nil {
		return fmt.Errorf("failed to encode transaction: %w", err)
	}

	return nil
}
