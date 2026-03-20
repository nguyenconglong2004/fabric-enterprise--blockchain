package network

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"

	"raft-order-service/internal/types"
)

// Transport handles all network communications
type Transport struct {
	Host   host.Host
	Ctx    context.Context
	Cancel context.CancelFunc
}

// NewTransport creates a new transport layer
func NewTransport(ctx context.Context, port int) (*Transport, error) {
	ctx, cancel := context.WithCancel(ctx)

	h, err := libp2p.New(
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create host: %w", err)
	}

	return &Transport{
		Host:   h,
		Ctx:    ctx,
		Cancel: cancel,
	}, nil
}

// NewClientTransport creates a transport for client (random port)
func NewClientTransport(ctx context.Context) (*Transport, error) {
	ctx, cancel := context.WithCancel(ctx)

	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create client host: %w", err)
	}

	return &Transport{
		Host:   h,
		Ctx:    ctx,
		Cancel: cancel,
	}, nil
}

// SetStreamHandler sets the handler for incoming streams
func (t *Transport) SetStreamHandler(handler network.StreamHandler) {
	t.Host.SetStreamHandler(protocol.ID(ProtocolID), handler)
}

// SetDeliverStreamHandler sets the handler for deliver streams
func (t *Transport) SetDeliverStreamHandler(handler network.StreamHandler) {
	t.Host.SetStreamHandler(protocol.ID(DeliverProtocolID), handler)
}

// SendMessage sends a message to a peer
func (t *Transport) SendMessage(peerID peer.ID, msg types.Message) error {
	s, err := t.Host.NewStream(t.Ctx, peerID, protocol.ID(ProtocolID))
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}
	defer s.Close()

	encoder := json.NewEncoder(s)
	if err := encoder.Encode(msg); err != nil {
		return fmt.Errorf("failed to encode message: %w", err)
	}

	return nil
}

// BroadcastMessage sends a message to all specified peers except self.
// Nếu onSendFailure != nil và gửi tới peer thất bại, gọi onSendFailure(peerID) thay vì chỉ log.
func (t *Transport) BroadcastMessage(msg types.Message, members []*types.MemberInfo, onSendFailure func(peer.ID)) {
	for _, member := range members {
		if member.PeerID == t.Host.ID() {
			continue
		}

		go func(pID peer.ID) {
			if err := t.SendMessage(pID, msg); err != nil {
				if onSendFailure != nil {
					onSendFailure(pID)
				} else {
					log.Printf("[%s] Error sending to %s: %v",
						t.Host.ID().ShortString(), pID.ShortString(), err)
				}
			}
		}(member.PeerID)
	}
}

// Connect connects to a peer
func (t *Transport) Connect(peerAddr string) (*peer.AddrInfo, error) {
	addr, err := peer.AddrInfoFromString(peerAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid peer address: %w", err)
	}

	if err := t.Host.Connect(t.Ctx, *addr); err != nil {
		return nil, fmt.Errorf("failed to connect to peer: %w", err)
	}

	return addr, nil
}

// ConnectToAddrInfo connects to a peer using AddrInfo
func (t *Transport) ConnectToAddrInfo(addrInfo peer.AddrInfo) error {
	return t.Host.Connect(t.Ctx, addrInfo)
}

// GetAddress returns the node's full address
func (t *Transport) GetAddress() string {
	addrs := t.Host.Addrs()
	if len(addrs) == 0 {
		return ""
	}
	return fmt.Sprintf("%s/p2p/%s", addrs[0], t.Host.ID())
}

// ID returns the peer ID
func (t *Transport) ID() peer.ID {
	return t.Host.ID()
}

// Addrs returns the host addresses
func (t *Transport) Addrs() []multiaddr.Multiaddr {
	return t.Host.Addrs()
}

// Peerstore returns the peerstore
func (t *Transport) Peerstore() peerstore.Peerstore {
	return t.Host.Peerstore()
}

// Close closes the transport
func (t *Transport) Close() {
	t.Cancel()
	t.Host.Close()
}
