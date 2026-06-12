package loadgen

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	netpkg "raft-order-service/internal/network"
	"raft-order-service/internal/types"
)

// deliverFromNew blocks replay of historical blocks — only commits after subscribe.
const deliverFromNew int64 = 1 << 60

// WatchDeliver subscribes to the orderer deliver stream and records each committed block.
// Works with cmd/server (no orchestrator required).
func WatchDeliver(ctx context.Context, transport *netpkg.Transport, orderer peer.AddrInfo, stats *CommitStats) error {
	if stats == nil {
		return fmt.Errorf("nil commit stats")
	}
	if err := transport.Host.Connect(ctx, orderer); err != nil {
		return fmt.Errorf("deliver: connect orderer: %w", err)
	}

	s, err := transport.Host.NewStream(ctx, orderer.ID, protocol.ID(netpkg.DeliverProtocolID))
	if err != nil {
		return fmt.Errorf("deliver: open stream: %w", err)
	}

	req := types.DeliverRequest{FromIndex: deliverFromNew}
	if err := json.NewEncoder(s).Encode(req); err != nil {
		s.Close()
		return fmt.Errorf("deliver: send request: %w", err)
	}

	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()

	decoder := json.NewDecoder(s)
	for {
		var block types.Block
		if err := decoder.Decode(&block); err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("deliver: decode block: %w", err)
			}
		}
		stats.record(len(block.Transactions), stats.now())
	}
}
