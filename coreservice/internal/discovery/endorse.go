package discovery

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"

	"coreservice/internal/network"
)

// EndorsementSender sends signed transactions to orderers.
type EndorsementSender interface {
	SendEndorsement(addr peer.AddrInfo, tx interface{}) error
}

// SendEndorsement delivers tx to orderers using cached membership; tries every alive peer.
// On total failure the cache is invalidated so the next call refetches membership.
func SendEndorsement(
	ctx context.Context,
	disc *Client,
	sender EndorsementSender,
	tx interface{},
) error {
	if disc == nil {
		return fmt.Errorf("discovery: nil client")
	}
	if sender == nil {
		return fmt.Errorf("discovery: nil endorsement sender")
	}

	trySend := func(mv *network.MembershipView) error {
		addrs, err := PickAllAliveOrdererAddrs(mv)
		if err != nil {
			return err
		}
		var lastErr error
		for _, addrStr := range addrs {
			ai, err := peer.AddrInfoFromString(addrStr)
			if err != nil {
				lastErr = err
				continue
			}
			if err := sender.SendEndorsement(*ai, tx); err != nil {
				lastErr = err
				continue
			}
			return nil
		}
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("discovery: no orderer accepted endorsement")
	}

	mv, err := disc.Snapshot(ctx)
	if err != nil {
		return err
	}
	if err := trySend(mv); err == nil {
		return nil
	}

	disc.Invalidate()
	mv, err = disc.Refresh(ctx)
	if err != nil {
		return err
	}
	return trySend(mv)
}
