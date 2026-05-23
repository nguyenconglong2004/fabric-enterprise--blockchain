package raft

import (
	"encoding/json"

	"github.com/libp2p/go-libp2p/core/network"

	"raft-order-service/internal/types"
)

// HandleEndorsementStream handles incoming endorsement transactions from Core Service
// This is called when an endorsement comes in via libp2p endorsement protocol
func (rn *RaftNode) HandleEndorsementStream(s network.Stream) {
	defer s.Close()

	decoder := json.NewDecoder(s)
	var tx types.Transaction

	if err := decoder.Decode(&tx); err != nil {
		rn.Logger.Printf("[%s] Error decoding endorsement: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	rn.Logger.Printf("[%s] Received endorsement for tx %s with %d endorsers",
		rn.Transport.ID().ShortString(), tx.Txid, len(tx.Endorsements))

	// Forward to leader if not leader
	if !rn.IsLeader() {
		leaderID := rn.GetLeaderID()
		if leaderID == "" {
			rn.Logger.Printf("[%s] Received endorsement but no leader known, dropping",
				rn.Transport.ID().ShortString())
			return
		}

		rn.Logger.Printf("[%s] Forwarding endorsement to leader %s",
			rn.Transport.ID().ShortString(), leaderID.ShortString())

		if err := rn.Transport.SendEndorsement(leaderID, tx); err != nil {
			rn.Logger.Printf("[%s] Failed to forward endorsement to leader: %v",
				rn.Transport.ID().ShortString(), err)
		}
		return
	}

	// Leader: add directly to TxPool
	if _, err := rn.SubmitTransaction(tx); err != nil {
		rn.Logger.Printf("[%s] Error submitting endorsement tx: %v",
			rn.Transport.ID().ShortString(), err)
		return
	}

	rn.Logger.Printf("[%s] Endorsement tx %s added to TxPool",
		rn.Transport.ID().ShortString(), tx.Txid)
}
