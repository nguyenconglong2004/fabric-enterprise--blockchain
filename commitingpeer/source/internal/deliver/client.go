package deliver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"commiting-peer/internal/types"
)

// DeliverProtocolID must match the constant in the ordering service.
const DeliverProtocolID = "/raft-order-service/deliver/1.0.0"

// Client holds a libp2p host used to connect to ordering service nodes.
type Client struct {
	host host.Host
}

// NewClient creates a new deliver client that listens on a random port.
func NewClient(ctx context.Context) (*Client, error) {
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
	)
	if err != nil {
		return nil, fmt.Errorf("deliver client: create libp2p host: %w", err)
	}
	return &Client{host: h}, nil
}

// Subscribe connects to an ordering service node at ordererAddr, sends a
// DeliverRequest starting at fromIndex, and launches a background goroutine
// that continuously reads incoming blocks from the stream and pushes each one
// into blockChan.
//
// The goroutine exits when ctx is cancelled or the stream is closed by the
// remote end. Callers should drain blockChan after cancellation.
func (c *Client) Subscribe(
	ctx context.Context,
	ordererAddr string,
	fromIndex int64,
	blockChan chan<- types.Block,
) error {
	addrInfo, err := peer.AddrInfoFromString(ordererAddr)
	if err != nil {
		return fmt.Errorf("deliver client: parse orderer address: %w", err)
	}

	if err := c.host.Connect(ctx, *addrInfo); err != nil {
		return fmt.Errorf("deliver client: connect to orderer: %w", err)
	}

	s, err := c.host.NewStream(ctx, addrInfo.ID, protocol.ID(DeliverProtocolID))
	if err != nil {
		return fmt.Errorf("deliver client: open deliver stream: %w", err)
	}

	// Send the deliver request to tell the orderer where to start.
	req := types.DeliverRequest{FromIndex: fromIndex}
	if err := json.NewEncoder(s).Encode(req); err != nil {
		s.Close()
		return fmt.Errorf("deliver client: send deliver request: %w", err)
	}

	log.Printf("[deliver] subscribed to %s from block index %d", addrInfo.ID.ShortString(), fromIndex)

	// Background goroutine: reads blocks off the stream and pushes them into
	// blockChan so the validation / commit pipeline can pick them up.
	go func() {
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

	return nil
}

// Close shuts down the underlying libp2p host.
func (c *Client) Close() {
	if err := c.host.Close(); err != nil {
		log.Printf("[deliver] close host: %v", err)
	}
}
