package deliver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"commiting-peer/internal/types"
)

// DeliverProtocolID must match the constant in the ordering service.
const DeliverProtocolID = "/raft-order-service/deliver/1.0.0"

// SyncProtocolID is used by ordering service clients to query UTXOs by address.
const SyncProtocolID = "/commiting-peer/sync/1.0.0"

// Client holds a libp2p host used to connect to ordering service nodes.
type Client struct {
	host         host.Host
	membershipCh chan *MembershipView
}

// NewClient creates a new deliver client that listens on a random port.
func NewClient(ctx context.Context) (*Client, error) {
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
	)
	if err != nil {
		return nil, fmt.Errorf("deliver client: create libp2p host: %w", err)
	}
	c := &Client{
		host:         h,
		membershipCh: make(chan *MembershipView, 1),
	}
	h.SetStreamHandler(protocol.ID(orderProtocolMain), c.handleOrderProtocolStream)
	return c, nil
}

func (c *Client) handleOrderProtocolStream(s network.Stream) {
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
	case c.membershipCh <- mv:
	default:
	}
}

// Subscribe connects to an ordering service node at ordererAddr, sends a
// DeliverRequest starting at fromIndex, and launches a background goroutine
// that continuously reads incoming blocks from the stream and pushes each one
// into blockChan.
//
// The returned channel is closed when the stream goroutine exits (disconnect or
// ctx cancel). Callers can wait on it to trigger reconnect.
func (c *Client) Subscribe(
	ctx context.Context,
	ordererAddr string,
	fromIndex int64,
	blockChan chan<- types.Block,
) (<-chan struct{}, error) {
	addrInfo, err := peer.AddrInfoFromString(ordererAddr)
	if err != nil {
		return nil, fmt.Errorf("deliver client: parse orderer address: %w", err)
	}

	if err := c.host.Connect(ctx, *addrInfo); err != nil {
		return nil, fmt.Errorf("deliver client: connect to orderer: %w", err)
	}

	s, err := c.host.NewStream(ctx, addrInfo.ID, protocol.ID(DeliverProtocolID))
	if err != nil {
		return nil, fmt.Errorf("deliver client: open deliver stream: %w", err)
	}

	// Send the deliver request to tell the orderer where to start.
	req := types.DeliverRequest{FromIndex: fromIndex}
	if err := json.NewEncoder(s).Encode(req); err != nil {
		s.Close()
		return nil, fmt.Errorf("deliver client: send deliver request: %w", err)
	}

	log.Printf("[deliver] subscribed to %s from block index %d", addrInfo.ID.ShortString(), fromIndex)

	done := make(chan struct{})

	// Background goroutine: reads blocks off the stream and pushes them into
	// blockChan so the validation / commit pipeline can pick them up.
	go func() {
		defer close(done)
		defer s.Close()

		// Close the stream when the context is cancelled so that the blocking
		// json.Decode call below returns promptly.
		go func() {
			<-ctx.Done()
			s.Close()
		}()

		decoder := json.NewDecoder(s)
		for {
			var block types.Block
			if err := decoder.Decode(&block); err != nil {
				if ctx.Err() == nil {
					// Unexpected disconnect — log only if we weren't shutting down.
					log.Printf("[deliver] stream from %s closed: %v",
						addrInfo.ID.ShortString(), err)
				}
				return
			}

			select {
			case blockChan <- block:
			case <-ctx.Done():
				return
			}
		}
	}()

	return done, nil
}

// SetStreamHandler registers a handler for the given protocol ID on the host.
// Used to register the sync protocol handler so external clients can query UTXOs.
func (c *Client) SetStreamHandler(protocolID string, handler network.StreamHandler) {
	c.host.SetStreamHandler(protocol.ID(protocolID), handler)
}

// GetAddress returns the first multiaddr of this host in the form
// /ip4/…/tcp/…/p2p/<peerID>, which callers can share with ordering service
// clients so they can open a sync stream.
func (c *Client) GetAddress() string {
	addrs := c.host.Addrs()
	if len(addrs) == 0 {
		return ""
	}
	return fmt.Sprintf("%s/p2p/%s", addrs[0], c.host.ID())
}

// Close shuts down the underlying libp2p host.
func (c *Client) Close() {
	if err := c.host.Close(); err != nil {
		log.Printf("[deliver] close host: %v", err)
	}
}
