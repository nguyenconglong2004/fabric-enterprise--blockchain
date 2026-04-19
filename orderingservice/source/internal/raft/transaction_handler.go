package raft

import (
	"encoding/json"
	"log"

	"github.com/libp2p/go-libp2p/core/network"

	"raft-order-service/internal/types"
)

// HandleTransactionStream handles incoming transactions from Core Service via P2P
// Protocol: /raft-order-service/transaction/1.0.0
func (rn *RaftNode) HandleTransactionStream(s network.Stream) {
	defer s.Close()

	decoder := json.NewDecoder(s)
	var tx types.Transaction

	if err := decoder.Decode(&tx); err != nil {
		log.Printf("[%s] transaction: failed to decode: %v",
			rn.Transport.ID().ShortString(), err)
		return
	}

	log.Printf("[%s] transaction: received %s from %s",
		rn.Transport.ID().ShortString(), tx.Txid[:16], s.Conn().RemotePeer().ShortString())

	// Validate transaction
	if err := tx.Validate(); err != nil {
		log.Printf("[%s] transaction: validation failed: %v",
			rn.Transport.ID().ShortString(), err)
		return
	}

	rn.TxPoolMu.Lock()
	rn.TxPool = append(rn.TxPool, tx)
	rn.TxPoolMu.Unlock()

	log.Printf("[%s] transaction: added to pool (size=%d)",
		rn.Transport.ID().ShortString(), len(rn.TxPool))

	// If leader, propose block automatically
	if rn.IsLeader() {
		log.Printf("[%s] transaction: I am leader, will propose block",
			rn.Transport.ID().ShortString())
	}
}
