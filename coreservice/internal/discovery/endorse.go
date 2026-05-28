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

// SendEndorsement delivers tx to the Raft leader when known, then other alive orderers.
// On total failure the cache is invalidated so the next call refetches membership.
// Set leaderOnly true to skip follower dial (faster under load).
func SendEndorsement(
	ctx context.Context,
	disc *Client,
	sender EndorsementSender,
	tx interface{},
	leaderOnly bool,
) error {
	if disc == nil {
		return fmt.Errorf("discovery: nil client")
	}
	if sender == nil {
		return fmt.Errorf("discovery: nil endorsement sender")
	}

	trySend := func(mv *network.MembershipView, leaderOnly bool) error {
		var lastErr error

		sendOne := func(addrStr string) error {
			ai, err := peer.AddrInfoFromString(addrStr)
			if err != nil {
				lastErr = err
				return err
			}
			if err := sender.SendEndorsement(*ai, tx); err != nil {
				lastErr = err
				return err
			}
			return nil
		}

		if leader, err := PickOrdererAddr(mv); err == nil {
			if err := sendOne(leader); err == nil {
				return nil
			}
		}
		if leaderOnly {
			if lastErr != nil {
				return lastErr
			}
			return fmt.Errorf("discovery: leader did not accept endorsement")
		}

		addrs, err := PickAllAliveOrdererAddrs(mv)
		if err != nil {
			if lastErr != nil {
				return lastErr
			}
			return err
		}
		for _, addrStr := range addrs {
			if err := sendOne(addrStr); err == nil {
				return nil
			}
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
	if err := trySend(mv, leaderOnly); err == nil {
		return nil
	}

	disc.Invalidate()
	mv, err = disc.Refresh(ctx)
	if err != nil {
		return err
	}
	return trySend(mv, leaderOnly)
}
