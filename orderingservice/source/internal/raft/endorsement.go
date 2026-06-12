package raft

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/libp2p/go-libp2p/core/network"

	"raft-order-service/internal/types"
)

// HandleEndorsementStream handles incoming endorsement transactions from Core Service.
// The stream is read in a loop so a single long-lived stream can carry many tx
// (OPT-3): at high TPS, opening one libp2p stream per tx saturates the host's
// stream-accept path and silently drops/resets streams, capping effective ingest.
// Reading many tx per stream removes that per-tx stream-churn ceiling. A legacy
// sender that sends exactly one tx then closes is still handled (one loop iteration
// followed by EOF).
func (rn *RaftNode) HandleEndorsementStream(s network.Stream) {
	defer s.Close()

	decoder := json.NewDecoder(s)
	for {
		var tx types.Transaction
		if err := decoder.Decode(&tx); err != nil {
			if !errors.Is(err, io.EOF) && rn.Transport.Ctx.Err() == nil {
				rn.Logger.Printf("[%s] Endorsement stream closed: %v", rn.Transport.ID().ShortString(), err)
			}
			return
		}

		// NOTE: no per-tx logging here. At high TPS the single log.Logger mutex (plus
		// console I/O) becomes a serialization point that the commit path also contends
		// for, stalling block commits during the load. Keep logging at block granularity.

		// Forward to leader if not leader.
		if !rn.IsLeader() {
			leaderID := rn.GetLeaderID()
			if leaderID == "" {
				continue
			}
			if err := rn.Transport.SendEndorsement(leaderID, tx); err != nil {
				rn.Logger.Printf("[%s] Failed to forward endorsement to leader: %v",
					rn.Transport.ID().ShortString(), err)
			}
			continue
		}

		// Leader: add directly to TxPool.
		if _, err := rn.SubmitTransaction(tx); err != nil {
			rn.Logger.Printf("[%s] Error submitting endorsement tx: %v",
				rn.Transport.ID().ShortString(), err)
		}
	}
}
