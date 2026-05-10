package raft

import (
	"encoding/json"
	"log"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"raft-order-service/internal/types"
)

// handleSyncStatusRequest trả lời peer đang sync về snapshot trạng thái hiện tại.
// Server không phục vụ nếu chính nó đang sync (tránh propagate dữ liệu chưa verify).
func (rn *RaftNode) handleSyncStatusRequest(msg types.Message) {
	rn.syncMu.Lock()
	syncing := rn.syncing
	rn.syncMu.Unlock()
	if syncing {
		log.Printf("[%s] sync: ignoring status request from %s — we are syncing",
			rn.Transport.ID().ShortString(), msg.SenderID)
		return
	}

	senderID, err := peer.Decode(msg.SenderID)
	if err != nil {
		return
	}

	rn.mu.RLock()
	currentTerm := rn.currentTerm
	leaderID := rn.currentLeaderID
	commitHash := append([]byte(nil), rn.lastCommittedHash...)
	rn.mu.RUnlock()

	resp := types.SyncStatusResponse{
		Term:              currentTerm,
		CommitIndex:       rn.OrderingBlock.GetLastIndex(),
		CommitHash:        commitHash,
		LogLastIndex:      rn.RaftLog.GetLastIndex(),
		MembershipVersion: rn.Membership.Version,
		LeaderID:          leaderID.String(),
	}

	respMsg := types.Message{
		Type:      types.MsgSyncStatusResponse,
		Term:      currentTerm,
		SenderID:  rn.Transport.ID().String(),
		Data:      resp,
		Timestamp: time.Now(),
	}

	if err := rn.Transport.SendMessage(senderID, respMsg); err != nil {
		log.Printf("[%s] sync: failed to send status response to %s: %v",
			rn.Transport.ID().ShortString(), senderID.ShortString(), err)
	}
}

// HandleSyncStream phục vụ một sync stream từ peer đang catch-up.
// Decode SyncDataRequest, stream block hoặc log entries trong [FromIndex..ToIndex]
// theo từng chunk SyncShardSize.
func (rn *RaftNode) HandleSyncStream(s network.Stream) {
	defer s.Close()

	rn.syncMu.Lock()
	syncing := rn.syncing
	rn.syncMu.Unlock()
	if syncing {
		log.Printf("[%s] sync: refusing sync stream — we are syncing", rn.Transport.ID().ShortString())
		return
	}

	decoder := json.NewDecoder(s)
	encoder := json.NewEncoder(s)

	var req types.SyncDataRequest
	if err := decoder.Decode(&req); err != nil {
		log.Printf("[%s] sync: failed to decode sync request: %v",
			rn.Transport.ID().ShortString(), err)
		return
	}

	log.Printf("[%s] sync: serving request kind=%d range=[%d..%d]",
		rn.Transport.ID().ShortString(), req.Kind, req.FromIndex, req.ToIndex)

	switch req.Kind {
	case types.SyncKindBlocks:
		rn.streamBlocks(encoder, req)
	case types.SyncKindLogEntries:
		rn.streamLogEntries(encoder, req)
	default:
		_ = encoder.Encode(types.SyncDataChunk{
			Kind: req.Kind,
			EOF:  true,
			Err:  "unknown sync kind",
		})
	}
}

// streamBlocks stream OrderingBlock[FromIndex..ToIndex] (1-based inclusive).
func (rn *RaftNode) streamBlocks(encoder *json.Encoder, req types.SyncDataRequest) {
	all := rn.OrderingBlock.GetBlocks()
	totalIdx := int64(len(all)) // 1-based last index = len

	from := req.FromIndex
	if from < 1 {
		from = 1
	}
	to := req.ToIndex
	if to > totalIdx {
		to = totalIdx
	}

	if from > to {
		_ = encoder.Encode(types.SyncDataChunk{Kind: types.SyncKindBlocks, EOF: true})
		return
	}

	chunkSize := int64(64)
	for cursor := from; cursor <= to; cursor += chunkSize {
		end := cursor + chunkSize - 1
		if end > to {
			end = to
		}
		// blocks slice is 0-based: index i in slice => blockIndex i+1
		batch := append([]types.Block(nil), all[cursor-1:end]...)
		chunk := types.SyncDataChunk{
			Kind:   types.SyncKindBlocks,
			Blocks: batch,
			EOF:    end == to,
		}
		if err := encoder.Encode(chunk); err != nil {
			log.Printf("[%s] sync: failed to send block chunk: %v",
				rn.Transport.ID().ShortString(), err)
			return
		}
	}
}

// streamLogEntries stream RaftLog entries có Index trong [FromIndex..ToIndex].
func (rn *RaftNode) streamLogEntries(encoder *json.Encoder, req types.SyncDataRequest) {
	all := rn.RaftLog.GetEntries()

	collected := make([]types.LogEntry, 0, len(all))
	for _, e := range all {
		if e.Index >= req.FromIndex && e.Index <= req.ToIndex {
			collected = append(collected, e)
		}
	}

	if len(collected) == 0 {
		_ = encoder.Encode(types.SyncDataChunk{Kind: types.SyncKindLogEntries, EOF: true})
		return
	}

	chunkSize := 64
	for i := 0; i < len(collected); i += chunkSize {
		end := i + chunkSize
		if end > len(collected) {
			end = len(collected)
		}
		batch := append([]types.LogEntry(nil), collected[i:end]...)
		chunk := types.SyncDataChunk{
			Kind:    types.SyncKindLogEntries,
			Entries: batch,
			EOF:     end == len(collected),
		}
		if err := encoder.Encode(chunk); err != nil {
			log.Printf("[%s] sync: failed to send log chunk: %v",
				rn.Transport.ID().ShortString(), err)
			return
		}
	}
}
