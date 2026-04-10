package raft

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"raft-order-service/internal/types"
)

// SubmitTransaction submits a signed blockchain transaction to the ordering service.
// If this node is not the leader, the transaction is forwarded to the leader.
func (rn *RaftNode) SubmitTransaction(tx types.Transaction) (string, error) {
	if err := tx.Validate(); err != nil {
		return "", fmt.Errorf("transaction validation failed: %w", err)
	}

	if !rn.IsLeader() {
		leaderID := rn.GetLeaderID()
		if leaderID == "" {
			return "", fmt.Errorf("no leader available")
		}
		return rn.forwardTxToLeader(tx, leaderID)
	}
	return rn.processTx(tx)
}

// processTx stores a transaction in the TxPool (leader only).
func (rn *RaftNode) processTx(tx types.Transaction) (string, error) {
	rn.TxPoolMu.Lock()
	rn.TxPool = append(rn.TxPool, tx)
	poolSize := len(rn.TxPool)
	rn.TxPoolMu.Unlock()

	log.Printf("[%s] Received tx %s (pool size: %d)",
		rn.Transport.ID().ShortString(), tx.Txid, poolSize)

	return tx.Txid, nil
}

// forwardTxToLeader forwards a transaction to the leader.
func (rn *RaftNode) forwardTxToLeader(tx types.Transaction, leaderID peer.ID) (string, error) {
	log.Printf("[%s] Forwarding tx %s to leader %s",
		rn.Transport.ID().ShortString(), tx.Txid, leaderID.ShortString())

	msg := types.Message{
		Type:      types.MsgTxRequest,
		Term:      rn.currentTerm,
		SenderID:  rn.Transport.ID().String(),
		Data:      tx,
		Timestamp: time.Now(),
	}

	if err := rn.Transport.SendMessage(leaderID, msg); err != nil {
		return "", fmt.Errorf("failed to forward to leader: %w", err)
	}

	return tx.Txid, nil
}

// HandleTxRequest handles a transaction request from a client or another node.
func (rn *RaftNode) HandleTxRequest(msg types.Message) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		log.Printf("[%s] Error marshaling tx data: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	var tx types.Transaction
	if err := json.Unmarshal(data, &tx); err != nil {
		log.Printf("[%s] Error unmarshaling tx: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	if err := tx.Validate(); err != nil {
		log.Printf("[%s] Transaction validation failed: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	if !rn.IsLeader() {
		leaderID := rn.GetLeaderID()
		if leaderID == "" {
			log.Printf("[%s] Received tx %s but no leader known, dropping",
				rn.Transport.ID().ShortString(), tx.Txid)
			return
		}
		fwdMsg := types.Message{
			Type:      types.MsgTxRequest,
			Term:      rn.currentTerm,
			SenderID:  rn.Transport.ID().String(),
			Data:      tx,
			Timestamp: time.Now(),
		}
		if err := rn.Transport.SendMessage(leaderID, fwdMsg); err != nil {
			log.Printf("[%s] Failed to forward tx %s to leader: %v",
				rn.Transport.ID().ShortString(), tx.Txid, err)
		}
		return
	}

	// We are the leader — store in TxPool
	rn.TxPoolMu.Lock()
	rn.TxPool = append(rn.TxPool, tx)
	poolSize := len(rn.TxPool)
	rn.TxPoolMu.Unlock()

	log.Printf("[%s] Accepted tx %s from client (pool size: %d)",
		rn.Transport.ID().ShortString(), tx.Txid, poolSize)

	// Send acknowledgment back to sender
	senderID, err := peer.Decode(msg.SenderID)
	if err == nil && senderID != rn.Transport.ID() {
		rn.mu.RLock()
		currentTerm := rn.currentTerm
		rn.mu.RUnlock()

		ackMsg := types.Message{
			Type:      types.MsgTxResponse,
			Term:      currentTerm,
			SenderID:  rn.Transport.ID().String(),
			Data:      tx,
			Timestamp: time.Now(),
		}
		if err := rn.Transport.SendMessage(senderID, ackMsg); err != nil {
			log.Printf("[%s] Failed to send tx ack to %s: %v",
				rn.Transport.ID().ShortString(), senderID.ShortString(), err)
		}
	}
}

// HandleTxResponse handles a transaction response (client-facing acknowledgment).
func (rn *RaftNode) HandleTxResponse(msg types.Message) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}
	var tx types.Transaction
	if err := json.Unmarshal(data, &tx); err != nil {
		return
	}
	log.Printf("[%s] Tx response received: %s",
		rn.Transport.ID().ShortString(), tx.Txid)
}

// ProposeBlock proposes a block using the first batchSize transactions from the TxPool (leader only).
func (rn *RaftNode) ProposeBlock(batchSize int) error {
	if !rn.IsLeader() {
		return fmt.Errorf("only leader can propose blocks")
	}

	rn.TxPoolMu.Lock()
	if len(rn.TxPool) == 0 {
		rn.TxPoolMu.Unlock()
		return fmt.Errorf("no transactions in pool")
	}
	count := batchSize
	if count > len(rn.TxPool) {
		count = len(rn.TxPool)
	}
	blockTxs := make([]types.Transaction, count)
	copy(blockTxs, rn.TxPool[:count])
	rn.TxPoolMu.Unlock()

	return rn.proposeBlockWithTxs(blockTxs)
}

// proposeBlockWithTxs creates a cryptographic block and broadcasts it to followers (leader only).
func (rn *RaftNode) proposeBlockWithTxs(blockTxs []types.Transaction) error {
	rn.mu.RLock()
	currentTerm := rn.currentTerm
	rn.mu.RUnlock()

	prevHash := rn.getLastCommittedHash()
	block := types.NewBlock(blockTxs, prevHash)
	blockID := block.BlockID()

	prevLogIndex := rn.RaftLog.GetLastIndex()

	entry := types.LogEntry{
		Index:        prevLogIndex + 1,
		PrevLogIndex: prevLogIndex,
		Term:         currentTerm,
		Type:         types.LogTypeBlockProposing,
		Block:        *block,
	}

	// Append to leader's own RaftLog
	rn.RaftLog.AppendEntry(entry)

	log.Printf("[%s] Proposing block %s (log index %d, prev %d) with %d tx",
		rn.Transport.ID().ShortString(), blockID, entry.Index, entry.PrevLogIndex, len(blockTxs))

	proposal := types.BlockProposal{
		Entry:     entry,
		Timestamp: time.Now(),
	}

	msg := types.Message{
		Type:      types.MsgBlockProposal,
		Term:      currentTerm,
		SenderID:  rn.Transport.ID().String(),
		Data:      proposal,
		Timestamp: time.Now(),
	}

	rn.BroadcastMessage(msg)

	// Block proposal acts as heartbeat
	rn.mu.Lock()
	rn.lastBlockSentTime = time.Now()
	rn.mu.Unlock()

	// Wait for ACKs from majority
	go rn.waitForBlockAcks(entry)

	return nil
}

// waitForBlockAcks waits for acknowledgments from followers for a block proposal.
func (rn *RaftNode) waitForBlockAcks(entry types.LogEntry) {
	acks := make(map[peer.ID]bool)
	acks[rn.Transport.ID()] = true // Count ourselves

	totalCount := rn.Membership.GetTotalCount()
	majority := totalCount/2 + 1

	blockID := entry.Block.BlockID()

	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	log.Printf("[%s] Waiting for block ACKs (need %d/%d)",
		rn.Transport.ID().ShortString(), majority, totalCount)

	for {
		select {
		case <-timeout:
			if len(acks) >= majority {
				log.Printf("[%s] Received majority ACKs (%d/%d) for block %s",
					rn.Transport.ID().ShortString(), len(acks), majority, blockID)
				rn.commitBlock(entry)
			} else {
				log.Printf("[%s] Failed to get majority ACKs (%d/%d) for block %s",
					rn.Transport.ID().ShortString(), len(acks), majority, blockID)
			}
			return

		case <-ticker.C:
			if len(acks) >= majority {
				log.Printf("[%s] Received majority ACKs (%d/%d) for block %s",
					rn.Transport.ID().ShortString(), len(acks), majority, blockID)
				rn.commitBlock(entry)
				return
			}

		case msg := <-rn.BlockAckChan:
			data, err := json.Marshal(msg.Data)
			if err != nil {
				continue
			}

			var ack types.BlockProposalAck
			if err := json.Unmarshal(data, &ack); err != nil {
				continue
			}

			if ack.LogIndex == entry.Index && ack.BlockID == blockID && ack.Accepted {
				senderID, err := peer.Decode(msg.SenderID)
				if err == nil {
					acks[senderID] = true
					log.Printf("[%s] Received ACK from %s for block %s (%d/%d)",
						rn.Transport.ID().ShortString(), senderID.ShortString(), blockID, len(acks), majority)
				}
			}
		}
	}
}

// ExecuteBlockTransactions logs committed transactions (UTXO state is managed by peer nodes, not the ordering service).
func (rn *RaftNode) ExecuteBlockTransactions(block types.Block) error {
	for _, tx := range block.Transactions {
		log.Printf("[%s] Block tx: %s (%d inputs, %d outputs)",
			rn.Transport.ID().ShortString(), tx.Txid, len(tx.Vin), len(tx.Vout))
	}
	return nil
}

// commitBlock commits a block after receiving majority ACKs (leader only).
func (rn *RaftNode) commitBlock(entry types.LogEntry) {
	block := entry.Block
	blockID := block.BlockID()

	log.Printf("[%s] Committing block %s (log index %d) with %d tx",
		rn.Transport.ID().ShortString(), blockID, entry.Index, len(block.Transactions))

	// Append block to OrderingBlock
	rn.OrderingBlock.AppendBlock(block)

	// Log committed transactions
	rn.ExecuteBlockTransactions(block)

	// Update the hash chain pointer
	rn.setLastCommittedHash(block.Hash)

	// Remove committed transactions from TxPool by Txid
	committedIDs := make(map[string]bool, len(block.Transactions))
	for _, tx := range block.Transactions {
		committedIDs[tx.Txid] = true
	}
	rn.TxPoolMu.Lock()
	remaining := rn.TxPool[:0]
	for _, tx := range rn.TxPool {
		if !committedIDs[tx.Txid] {
			remaining = append(remaining, tx)
		}
	}
	rn.TxPool = remaining
	rn.TxPoolMu.Unlock()

	// Broadcast commit to followers
	commit := types.BlockCommit{
		BlockID:   blockID,
		LogIndex:  entry.Index,
		Timestamp: time.Now(),
	}

	rn.mu.RLock()
	currentTerm := rn.currentTerm
	rn.mu.RUnlock()

	msg := types.Message{
		Type:      types.MsgBlockCommit,
		Term:      currentTerm,
		SenderID:  rn.Transport.ID().String(),
		Data:      commit,
		Timestamp: time.Now(),
	}

	rn.BroadcastMessage(msg)

	// Block commit acts as heartbeat
	rn.mu.Lock()
	rn.lastBlockSentTime = time.Now()
	rn.mu.Unlock()

	log.Printf("[%s] Block %s committed (hash: %s, merkle: %s, ordering blocks: %d)",
		rn.Transport.ID().ShortString(), blockID,
		hex.EncodeToString(block.Hash[:4]),
		hex.EncodeToString(block.MerkleRoot[:4]),
		rn.OrderingBlock.GetLastIndex())

	// Signal auto-propose goroutine (non-blocking)
	select {
	case rn.blockCommittedNotify <- struct{}{}:
	default:
	}
}

// HandleBlockProposal handles a block proposal from the leader (follower only).
func (rn *RaftNode) HandleBlockProposal(msg types.Message) {
	if rn.IsLeader() {
		return
	}

	data, err := json.Marshal(msg.Data)
	if err != nil {
		log.Printf("[%s] Error marshaling block proposal: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	var proposal types.BlockProposal
	if err := json.Unmarshal(data, &proposal); err != nil {
		log.Printf("[%s] Error unmarshaling block proposal: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	entry := proposal.Entry
	blockID := entry.Block.BlockID()

	rn.mu.RLock()
	currentTerm := rn.currentTerm
	leaderID := rn.currentLeaderID
	rn.mu.RUnlock()

	senderID, err := peer.Decode(msg.SenderID)
	if err != nil {
		return
	}

	if senderID != leaderID {
		log.Printf("[%s] Block proposal from non-leader %s, ignoring",
			rn.Transport.ID().ShortString(), senderID.ShortString())
		return
	}

	if msg.Term < currentTerm {
		log.Printf("[%s] Block proposal with old term %d (current: %d), rejecting",
			rn.Transport.ID().ShortString(), msg.Term, currentTerm)
		rn.sendBlockProposalAck(blockID, entry.Index, false, "old term")
		return
	}

	// Block proposal from leader counts as heartbeat
	rn.updateLastHeartbeat()

	// Check log continuity
	lastIndex := rn.RaftLog.GetLastIndex()
	if entry.PrevLogIndex != lastIndex {
		reason := fmt.Sprintf("log discontinuity: expected PrevLogIndex=%d but got %d (our last index=%d)",
			lastIndex, entry.PrevLogIndex, lastIndex)
		log.Printf("[%s] Rejecting block %s: %s",
			rn.Transport.ID().ShortString(), blockID, reason)
		rn.sendBlockProposalAck(blockID, entry.Index, false, reason)
		return
	}

	if entry.PrevLogIndex > 0 {
		prevEntry := rn.RaftLog.FindEntryByIndex(entry.PrevLogIndex)
		if prevEntry == nil {
			reason := fmt.Sprintf("previous log entry %d not found", entry.PrevLogIndex)
			log.Printf("[%s] Rejecting block %s: %s",
				rn.Transport.ID().ShortString(), blockID, reason)
			rn.sendBlockProposalAck(blockID, entry.Index, false, reason)
			return
		}
		if prevEntry.Term != entry.Term {
			rn.RaftLog.RemoveFrom(entry.PrevLogIndex)
			reason := fmt.Sprintf("term mismatch on prev entry %d: have %d, expected ~%d",
				entry.PrevLogIndex, prevEntry.Term, entry.Term)
			log.Printf("[%s] Rejecting block %s: %s",
				rn.Transport.ID().ShortString(), blockID, reason)
			rn.sendBlockProposalAck(blockID, entry.Index, false, reason)
			return
		}
	}

	rn.RaftLog.AppendEntry(entry)

	log.Printf("[%s] Accepted block proposal %s (log index %d, prev %d, %d tx)",
		rn.Transport.ID().ShortString(), blockID, entry.Index, entry.PrevLogIndex, len(entry.Block.Transactions))

	rn.sendBlockProposalAck(blockID, entry.Index, true, "")
}

// sendBlockProposalAck sends an acknowledgment for a block proposal.
func (rn *RaftNode) sendBlockProposalAck(blockID string, logIndex int64, accepted bool, reason string) {
	ack := types.BlockProposalAck{
		BlockID:   blockID,
		LogIndex:  logIndex,
		Accepted:  accepted,
		Reason:    reason,
		Timestamp: time.Now(),
	}

	rn.mu.RLock()
	currentTerm := rn.currentTerm
	leaderID := rn.currentLeaderID
	rn.mu.RUnlock()

	if leaderID == "" {
		return
	}

	msg := types.Message{
		Type:      types.MsgBlockProposalAck,
		Term:      currentTerm,
		SenderID:  rn.Transport.ID().String(),
		Data:      ack,
		Timestamp: time.Now(),
	}

	if err := rn.Transport.SendMessage(leaderID, msg); err != nil {
		log.Printf("[%s] Error sending block proposal ACK: %v", rn.Transport.ID().ShortString(), err)
	}
}

// HandleBlockProposalAck handles acknowledgment for a block proposal (leader only).
func (rn *RaftNode) HandleBlockProposalAck(msg types.Message) {
	if !rn.IsLeader() {
		return
	}

	select {
	case rn.BlockAckChan <- msg:
	default:
		log.Printf("[%s] Block ACK channel full, dropping ACK", rn.Transport.ID().ShortString())
	}
}

// HandleBlockCommit handles a block commit notification from the leader (follower only).
func (rn *RaftNode) HandleBlockCommit(msg types.Message) {
	if rn.IsLeader() {
		return
	}

	data, err := json.Marshal(msg.Data)
	if err != nil {
		log.Printf("[%s] Error marshaling block commit: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	var commit types.BlockCommit
	if err := json.Unmarshal(data, &commit); err != nil {
		log.Printf("[%s] Error unmarshaling block commit: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	senderID, err := peer.Decode(msg.SenderID)
	if err != nil {
		return
	}

	rn.mu.RLock()
	currentTerm := rn.currentTerm
	leaderID := rn.currentLeaderID
	rn.mu.RUnlock()

	if senderID != leaderID {
		log.Printf("[%s] Block commit from non-leader %s, ignoring",
			rn.Transport.ID().ShortString(), senderID.ShortString())
		return
	}

	if msg.Term < currentTerm {
		log.Printf("[%s] Block commit with old term %d (current: %d), ignoring",
			rn.Transport.ID().ShortString(), msg.Term, currentTerm)
		return
	}

	rn.updateLastHeartbeat()

	// Find the log entry to commit
	entry := rn.RaftLog.FindEntryByIndex(commit.LogIndex)
	if entry == nil {
		log.Printf("[%s] Warning: log entry %d for block %s not found in RaftLog",
			rn.Transport.ID().ShortString(), commit.LogIndex, commit.BlockID)
		return
	}

	// Verify the block ID matches
	if entry.Block.BlockID() != commit.BlockID {
		log.Printf("[%s] Warning: block ID mismatch at log index %d: have %s, got %s",
			rn.Transport.ID().ShortString(), commit.LogIndex, entry.Block.BlockID(), commit.BlockID)
		return
	}

	rn.OrderingBlock.AppendBlock(entry.Block)
	rn.setLastCommittedHash(entry.Block.Hash)
	rn.DeliverMgr.NotifyNewBlock(entry.Block)

	log.Printf("[%s] Committed block %s (log index %d) — ordering blocks: %d",
		rn.Transport.ID().ShortString(), commit.BlockID, commit.LogIndex, rn.OrderingBlock.GetLastIndex())
}

// StartAutoProposeBlock starts an auto-propose loop on the leader.
func (rn *RaftNode) StartAutoProposeBlock(batchSize int) error {
	if !rn.IsLeader() {
		return fmt.Errorf("only leader can auto-propose blocks")
	}

	rn.autoProposeMu.Lock()
	if rn.autoProposeRunning {
		rn.autoProposeMu.Unlock()
		return fmt.Errorf("auto-propose already running")
	}
	rn.autoProposeRunning = true
	rn.autoProposeStop = make(chan struct{})
	stopChan := rn.autoProposeStop
	rn.autoProposeMu.Unlock()

	log.Printf("[%s] Auto-propose started (batch size: %d tx/block)",
		rn.Transport.ID().ShortString(), batchSize)

	go func() {
		defer func() {
			rn.autoProposeMu.Lock()
			rn.autoProposeRunning = false
			rn.autoProposeMu.Unlock()
			log.Printf("[%s] Auto-propose goroutine exited", rn.Transport.ID().ShortString())
		}()

		for {
			select {
			case <-stopChan:
				return
			default:
			}

			if !rn.IsLeader() {
				log.Printf("[%s] Auto-propose: stepped down from leader, stopping",
					rn.Transport.ID().ShortString())
				return
			}

			rn.TxPoolMu.Lock()
			poolSize := len(rn.TxPool)
			rn.TxPoolMu.Unlock()

			if poolSize == 0 {
				select {
				case <-stopChan:
					return
				case <-time.After(200 * time.Millisecond):
				}
				continue
			}

			log.Printf("[%s] Auto-propose: proposing block with up to %d tx (pool: %d)",
				rn.Transport.ID().ShortString(), batchSize, poolSize)

			if err := rn.ProposeBlock(batchSize); err != nil {
				log.Printf("[%s] Auto-propose: ProposeBlock error: %v",
					rn.Transport.ID().ShortString(), err)
				select {
				case <-stopChan:
					return
				case <-time.After(500 * time.Millisecond):
				}
				continue
			}

			select {
			case <-stopChan:
				return
			case <-rn.blockCommittedNotify:
				log.Printf("[%s] Auto-propose: block committed, checking next batch",
					rn.Transport.ID().ShortString())
			case <-time.After(10 * time.Second):
				log.Printf("[%s] Auto-propose: timeout waiting for block commit, retrying",
					rn.Transport.ID().ShortString())
			}
		}
	}()

	return nil
}

// StopAutoProposeBlock stops the auto-propose loop.
func (rn *RaftNode) StopAutoProposeBlock() {
	rn.autoProposeMu.Lock()
	defer rn.autoProposeMu.Unlock()

	if !rn.autoProposeRunning || rn.autoProposeStop == nil {
		return
	}
	close(rn.autoProposeStop)
	rn.autoProposeStop = nil
	log.Printf("[%s] Auto-propose stop signal sent", rn.Transport.ID().ShortString())
}

// IsAutoProposeRunning returns true if the auto-propose loop is active.
func (rn *RaftNode) IsAutoProposeRunning() bool {
	rn.autoProposeMu.Lock()
	defer rn.autoProposeMu.Unlock()
	return rn.autoProposeRunning
}

// getLastCommittedHash returns the hash of the last committed block (thread-safe).
func (rn *RaftNode) getLastCommittedHash() []byte {
	rn.mu.RLock()
	defer rn.mu.RUnlock()
	return rn.lastCommittedHash
}

// setLastCommittedHash updates the hash chain pointer (thread-safe).
func (rn *RaftNode) setLastCommittedHash(h []byte) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	rn.lastCommittedHash = h
}

// PrintStatus prints the current status of the node to the given writer.
func (rn *RaftNode) PrintStatus(w ...io.Writer) {
	var out io.Writer = os.Stdout
	if len(w) > 0 && w[0] != nil {
		out = w[0]
	}

	rn.mu.RLock()
	state := rn.state
	term := rn.currentTerm
	leaderID := rn.currentLeaderID
	lastHash := rn.lastCommittedHash
	rn.mu.RUnlock()

	leaderStr := "none"
	if leaderID != "" {
		leaderStr = leaderID.ShortString()
	}

	lastHashStr := "(none)"
	if len(lastHash) > 0 {
		lastHashStr = hex.EncodeToString(lastHash)
	}

	fmt.Fprintf(out, "\n=== Node Status ===\n")
	fmt.Fprintf(out, "Node ID:    %s\n", rn.Transport.ID().ShortString())
	fmt.Fprintf(out, "State:      %s\n", state)
	fmt.Fprintf(out, "Term:       %d\n", term)
	fmt.Fprintf(out, "Leader:     %s\n", leaderStr)
	fmt.Fprintf(out, "Address:    %s\n", rn.GetAddress())
	fmt.Fprintf(out, "Last hash:  %s\n", lastHashStr)

	fmt.Fprintf(out, "\n=== Membership ===\n")
	members := rn.Membership.GetAliveMembers()
	fmt.Fprintf(out, "Alive members: %d\n", len(members))
	for _, member := range members {
		tags := ""
		if member.PeerID == leaderID {
			tags += " (LEADER)"
		}
		if member.PeerID == rn.Transport.ID() {
			tags += " (SELF)"
		}
		fmt.Fprintf(out, "  - Priority %d: %s%s\n", member.Priority, member.PeerID.ShortString(), tags)
	}

	fmt.Fprintf(out, "\n=== Tx Pool (pending) ===\n")
	rn.TxPoolMu.Lock()
	fmt.Fprintf(out, "Pending tx: %d\n", len(rn.TxPool))
	for i, tx := range rn.TxPool {
		fmt.Fprintf(out, "  %d. %s (%d in, %d out)\n", i+1, tx.Txid, len(tx.Vin), len(tx.Vout))
	}
	rn.TxPoolMu.Unlock()

	fmt.Fprintf(out, "\n=== Raft Log (uncommitted) ===\n")
	entries := rn.RaftLog.GetEntries()
	fmt.Fprintf(out, "Uncommitted entries: %d\n", len(entries))
	for _, e := range entries {
		fmt.Fprintf(out, "  [%d] term=%d prev=%d block=%s (%d tx)\n",
			e.Index, e.Term, e.PrevLogIndex, e.Block.BlockID(), len(e.Block.Transactions))
	}

	fmt.Fprintf(out, "\n=== Ordering Blocks (committed) ===\n")
	blocks := rn.OrderingBlock.GetBlocks()
	fmt.Fprintf(out, "Committed blocks: %d\n", len(blocks))
	for i, b := range blocks {
		fmt.Fprintf(out, "  Block #%d: %s (%d tx)\n", i+1, b.BlockID(), len(b.Transactions))
		for j, tx := range b.Transactions {
			fmt.Fprintf(out, "    %d. %s (%d in, %d out)\n", j+1, tx.Txid, len(tx.Vin), len(tx.Vout))
		}
	}
	fmt.Fprintf(out, "==================\n\n")
}
