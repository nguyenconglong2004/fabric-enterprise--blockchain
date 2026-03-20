package raft

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"raft-order-service/internal/types"
)

// SubmitTransaction submits a new transaction to the service.
// If this node is not the leader, it forwards the request to the leader.
func (rn *RaftNode) SubmitTransaction(txType types.TransactionType, txData interface{}) (string, error) {
	// Create transaction using factory
	tx := types.TransactionFactory(txType)
	if tx == nil {
		return "", fmt.Errorf("unsupported transaction type: %s", txType)
	}

	// Unmarshal data into transaction
	dataBytes, err := json.Marshal(txData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal transaction data: %w", err)
	}
	if err := json.Unmarshal(dataBytes, tx); err != nil {
		return "", fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	// Validate transaction
	if err := tx.Validate(); err != nil {
		return "", fmt.Errorf("transaction validation failed: %w", err)
	}

	if !rn.IsLeader() {
		leaderID := rn.GetLeaderID()
		if leaderID == "" {
			return "", fmt.Errorf("no leader available")
		}
		return rn.forwardTxToLeader(txType, tx, leaderID)
	}
	return rn.processTx(txType, tx)
}

// processTx stores a transaction in the TxPool (leader only)
func (rn *RaftNode) processTx(txType types.TransactionType, tx types.Transaction) (string, error) {
	wrapper := types.TransactionWrapper{
		Type:        txType,
		Transaction: tx,
		ReceivedAt:  time.Now(),
	}

	rn.TxPoolMu.Lock()
	rn.TxPool = append(rn.TxPool, wrapper)
	poolSize := len(rn.TxPool)
	rn.TxPoolMu.Unlock()

	log.Printf("[%s] Received tx %s (type: %s, pool size: %d)",
		rn.Transport.ID().ShortString(), tx.GetID(), txType, poolSize)

	return tx.GetID(), nil
}

// forwardTxToLeader forwards a transaction to the leader
func (rn *RaftNode) forwardTxToLeader(txType types.TransactionType, tx types.Transaction, leaderID peer.ID) (string, error) {
	log.Printf("[%s] Forwarding tx %s (type: %s) to leader %s",
		rn.Transport.ID().ShortString(), tx.GetID(), txType, leaderID.ShortString())

	wrapper := types.TransactionWrapper{
		Type:        txType,
		Transaction: tx,
		ReceivedAt:  time.Now(),
	}

	msg := types.Message{
		Type:      types.MsgTxRequest,
		Term:      rn.currentTerm,
		SenderID:  rn.Transport.ID().String(),
		Data:      wrapper,
		Timestamp: time.Now(),
	}

	if err := rn.Transport.SendMessage(leaderID, msg); err != nil {
		return "", fmt.Errorf("failed to forward to leader: %w", err)
	}

	return tx.GetID(), nil
}

// HandleTxRequest handles a transaction request from a client or another node
func (rn *RaftNode) HandleTxRequest(msg types.Message) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		log.Printf("[%s] Error marshaling tx data: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	var wrapper types.TransactionWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		log.Printf("[%s] Error unmarshaling tx wrapper: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	// Recreate transaction using factory
	tx := types.TransactionFactory(wrapper.Type)
	if tx == nil {
		log.Printf("[%s] Unknown transaction type: %s", rn.Transport.ID().ShortString(), wrapper.Type)
		return
	}

	// Unmarshal transaction data
	txData, _ := json.Marshal(wrapper.Transaction)
	if err := json.Unmarshal(txData, tx); err != nil {
		log.Printf("[%s] Error unmarshaling tx: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	// Validate transaction
	if err := tx.Validate(); err != nil {
		log.Printf("[%s] Transaction validation failed: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	if !rn.IsLeader() {
		// Forward to leader if we know who it is
		leaderID := rn.GetLeaderID()
		if leaderID == "" {
			log.Printf("[%s] Received tx %s but no leader known, dropping",
				rn.Transport.ID().ShortString(), tx.GetID())
			return
		}
		fwdMsg := types.Message{
			Type:      types.MsgTxRequest,
			Term:      rn.currentTerm,
			SenderID:  rn.Transport.ID().String(),
			Data:      wrapper,
			Timestamp: time.Now(),
		}
		if err := rn.Transport.SendMessage(leaderID, fwdMsg); err != nil {
			log.Printf("[%s] Failed to forward tx %s to leader: %v",
				rn.Transport.ID().ShortString(), tx.GetID(), err)
		}
		return
	}

	// We are the leader — store in TxPool
	wrapper.ReceivedAt = time.Now()
	rn.TxPoolMu.Lock()
	rn.TxPool = append(rn.TxPool, wrapper)
	poolSize := len(rn.TxPool)
	rn.TxPoolMu.Unlock()

	log.Printf("[%s] Accepted tx %s (type: %s) from client (pool size: %d)",
		rn.Transport.ID().ShortString(), tx.GetID(), wrapper.Type, poolSize)

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
			Data:      wrapper,
			Timestamp: time.Now(),
		}
		if err := rn.Transport.SendMessage(senderID, ackMsg); err != nil {
			log.Printf("[%s] Failed to send tx ack to %s: %v",
				rn.Transport.ID().ShortString(), senderID.ShortString(), err)
		}
	}
}

// HandleTxResponse handles a transaction response (client-facing acknowledgment)
func (rn *RaftNode) HandleTxResponse(msg types.Message) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}
	var wrapper types.TransactionWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return
	}

	tx := types.TransactionFactory(wrapper.Type)
	if tx == nil {
		return
	}
	txData, _ := json.Marshal(wrapper.Transaction)
	if err := json.Unmarshal(txData, tx); err != nil {
		return
	}

	log.Printf("[%s] Tx response received: %s (type: %s)",
		rn.Transport.ID().ShortString(), tx.GetID(), wrapper.Type)
}

// ProposeBlock proposes a block using the first batchSize transactions from the TxPool (leader only)
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
	blockTxs := make([]types.TransactionWrapper, count)
	copy(blockTxs, rn.TxPool[:count])
	rn.TxPoolMu.Unlock()

	return rn.proposeBlockWithTxs(blockTxs)
}

// proposeBlockWithTxs creates a log entry and broadcasts it to followers (leader only)
func (rn *RaftNode) proposeBlockWithTxs(blockTxs []types.TransactionWrapper) error {
	rn.mu.RLock()
	currentTerm := rn.currentTerm
	rn.mu.RUnlock()

	block := types.Block{
		BlockID:      fmt.Sprintf("block-%d", time.Now().UnixNano()),
		Transactions: blockTxs,
		Timestamp:    time.Now(),
	}

	prevLogIndex := rn.RaftLog.GetLastIndex()

	entry := types.LogEntry{
		Index:        prevLogIndex + 1,
		PrevLogIndex: prevLogIndex,
		Term:         currentTerm,
		Type:         types.LogTypeBlockProposing,
		Block:        block,
	}

	// Append to leader's own RaftLog
	rn.RaftLog.AppendEntry(entry)

	log.Printf("[%s] Proposing block %s (log index %d, prev %d) with %d tx",
		rn.Transport.ID().ShortString(), block.BlockID, entry.Index, entry.PrevLogIndex, len(blockTxs))

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

// waitForBlockAcks waits for acknowledgments from followers for a block proposal
func (rn *RaftNode) waitForBlockAcks(entry types.LogEntry) {
	acks := make(map[peer.ID]bool)
	acks[rn.Transport.ID()] = true // Count ourselves

	aliveCount := len(rn.Membership.GetAliveMembers())
	majority := aliveCount/2 + 1

	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	log.Printf("[%s] Waiting for block ACKs (need %d/%d)",
		rn.Transport.ID().ShortString(), majority, aliveCount)

	for {
		select {
		case <-timeout:
			if len(acks) >= majority {
				log.Printf("[%s] Received majority ACKs (%d/%d) for block %s",
					rn.Transport.ID().ShortString(), len(acks), majority, entry.Block.BlockID)
				rn.commitBlock(entry)
			} else {
				log.Printf("[%s] Failed to get majority ACKs (%d/%d) for block %s",
					rn.Transport.ID().ShortString(), len(acks), majority, entry.Block.BlockID)
			}
			return

		case <-ticker.C:
			if len(acks) >= majority {
				log.Printf("[%s] Received majority ACKs (%d/%d) for block %s",
					rn.Transport.ID().ShortString(), len(acks), majority, entry.Block.BlockID)
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

			if ack.LogIndex == entry.Index && ack.BlockID == entry.Block.BlockID && ack.Accepted {
				senderID, err := peer.Decode(msg.SenderID)
				if err == nil {
					acks[senderID] = true
					log.Printf("[%s] Received ACK from %s for block %s (%d/%d)",
						rn.Transport.ID().ShortString(), senderID.ShortString(), entry.Block.BlockID, len(acks), majority)
				}
			}
		}
	}
}

// ExecuteBlockTransactions executes all transactions in a committed block
func (rn *RaftNode) ExecuteBlockTransactions(block types.Block) error {
	for _, wrapper := range block.Transactions {
		tx := types.TransactionFactory(wrapper.Type)
		if tx == nil {
			log.Printf("[%s] Unknown transaction type: %s",
				rn.Transport.ID().ShortString(), wrapper.Type)
			continue
		}

		txBytes, _ := json.Marshal(wrapper.Transaction)
		if err := json.Unmarshal(txBytes, tx); err != nil {
			log.Printf("[%s] Failed to unmarshal tx: %v",
				rn.Transport.ID().ShortString(), err)
			continue
		}

		// Execute transaction
		if err := tx.Execute(); err != nil {
			log.Printf("[%s] Failed to execute tx %s: %v",
				rn.Transport.ID().ShortString(), tx.GetID(), err)
			// Continue with other transactions even if one fails
		} else {
			log.Printf("[%s] Successfully executed tx %s (type: %s)",
				rn.Transport.ID().ShortString(), tx.GetID(), wrapper.Type)
		}
	}
	return nil
}

// commitBlock commits a block after receiving majority ACKs (leader only)
func (rn *RaftNode) commitBlock(entry types.LogEntry) {
	block := entry.Block

	log.Printf("[%s] Committing block %s (log index %d) with %d tx",
		rn.Transport.ID().ShortString(), block.BlockID, entry.Index, len(block.Transactions))

	// Append block to OrderingBlock (log entry is kept in RaftLog)
	rn.OrderingBlock.AppendBlock(block)

	// Execute transactions in the block
	rn.ExecuteBlockTransactions(block)

	// Remove committed transactions from TxPool
	committedIDs := make(map[string]bool, len(block.Transactions))
	for _, wrapper := range block.Transactions {
		if wrapper.Transaction != nil {
			committedIDs[wrapper.Transaction.GetID()] = true
		}
	}
	rn.TxPoolMu.Lock()
	remaining := rn.TxPool[:0]
	for _, wrapper := range rn.TxPool {
		if wrapper.Transaction != nil && !committedIDs[wrapper.Transaction.GetID()] {
			remaining = append(remaining, wrapper)
		}
	}
	rn.TxPool = remaining
	rn.TxPoolMu.Unlock()

	// Broadcast commit to followers
	commit := types.BlockCommit{
		BlockID:   block.BlockID,
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

	log.Printf("[%s] Block %s committed (ordering blocks: %d)",
		rn.Transport.ID().ShortString(), block.BlockID, rn.OrderingBlock.GetLastIndex())

	// Signal auto-propose goroutine (non-blocking)
	select {
	case rn.blockCommittedNotify <- struct{}{}:
	default:
	}
}

// HandleBlockProposal handles a block proposal from the leader (follower only)
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

	rn.mu.RLock()
	currentTerm := rn.currentTerm
	leaderID := rn.currentLeaderID
	rn.mu.RUnlock()

	// Verify sender is current leader
	senderID, err := peer.Decode(msg.SenderID)
	if err != nil {
		return
	}

	if senderID != leaderID {
		log.Printf("[%s] Block proposal from non-leader %s, ignoring",
			rn.Transport.ID().ShortString(), senderID.ShortString())
		return
	}

	// Verify term
	if msg.Term < currentTerm {
		log.Printf("[%s] Block proposal with old term %d (current: %d), rejecting",
			rn.Transport.ID().ShortString(), msg.Term, currentTerm)
		rn.sendBlockProposalAck(entry.Block.BlockID, entry.Index, false, "old term")
		return
	}

	// Block proposal from leader counts as heartbeat
	rn.updateLastHeartbeat()

	// Check log continuity: PrevLogIndex must match our last index
	lastIndex := rn.RaftLog.GetLastIndex()
	if entry.PrevLogIndex != lastIndex {
		reason := fmt.Sprintf("log discontinuity: expected PrevLogIndex=%d but got %d (our last index=%d)",
			lastIndex, entry.PrevLogIndex, lastIndex)
		log.Printf("[%s] Rejecting block %s: %s",
			rn.Transport.ID().ShortString(), entry.Block.BlockID, reason)
		rn.sendBlockProposalAck(entry.Block.BlockID, entry.Index, false, reason)
		return
	}

	// If PrevLogIndex > 0, verify term of previous entry matches
	if entry.PrevLogIndex > 0 {
		prevEntry := rn.RaftLog.FindEntryByIndex(entry.PrevLogIndex)
		if prevEntry == nil {
			reason := fmt.Sprintf("previous log entry %d not found", entry.PrevLogIndex)
			log.Printf("[%s] Rejecting block %s: %s",
				rn.Transport.ID().ShortString(), entry.Block.BlockID, reason)
			rn.sendBlockProposalAck(entry.Block.BlockID, entry.Index, false, reason)
			return
		}
		if prevEntry.Term != entry.Term {
			// Term mismatch on previous entry — truncate and reject
			rn.RaftLog.RemoveFrom(entry.PrevLogIndex)
			reason := fmt.Sprintf("term mismatch on prev entry %d: have %d, expected ~%d",
				entry.PrevLogIndex, prevEntry.Term, entry.Term)
			log.Printf("[%s] Rejecting block %s: %s",
				rn.Transport.ID().ShortString(), entry.Block.BlockID, reason)
			rn.sendBlockProposalAck(entry.Block.BlockID, entry.Index, false, reason)
			return
		}
	}

	// All checks passed — append to RaftLog
	rn.RaftLog.AppendEntry(entry)

	log.Printf("[%s] Accepted block proposal %s (log index %d, prev %d, %d tx)",
		rn.Transport.ID().ShortString(), entry.Block.BlockID, entry.Index, entry.PrevLogIndex, len(entry.Block.Transactions))

	rn.sendBlockProposalAck(entry.Block.BlockID, entry.Index, true, "")
}

// sendBlockProposalAck sends an acknowledgment for a block proposal
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

// HandleBlockProposalAck handles acknowledgment for a block proposal (leader only)
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

// HandleBlockCommit handles a block commit notification from the leader (follower only)
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

	// Verify sender is current leader
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

	// Block commit from leader counts as heartbeat
	rn.updateLastHeartbeat()

	// Find the log entry to commit
	entry := rn.RaftLog.FindEntryByIndex(commit.LogIndex)
	if entry == nil {
		log.Printf("[%s] Warning: log entry %d for block %s not found in RaftLog",
			rn.Transport.ID().ShortString(), commit.LogIndex, commit.BlockID)
		return
	}

	// Append block to OrderingBlock (log entry is kept in RaftLog)
	rn.OrderingBlock.AppendBlock(entry.Block)
	rn.DeliverMgr.NotifyNewBlock(entry.Block)

	log.Printf("[%s] Committed block %s (log index %d) — ordering blocks: %d",
		rn.Transport.ID().ShortString(), commit.BlockID, commit.LogIndex, rn.OrderingBlock.GetLastIndex())
}

// StartAutoProposeBlock starts an auto-propose loop on the leader
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

			// Wait for block commit confirmation before proposing the next batch
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

// StopAutoProposeBlock stops the auto-propose loop
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

// IsAutoProposeRunning returns true if the auto-propose loop is active
func (rn *RaftNode) IsAutoProposeRunning() bool {
	rn.autoProposeMu.Lock()
	defer rn.autoProposeMu.Unlock()
	return rn.autoProposeRunning
}

// PrintStatus prints the current status of the node to the given writer.
// Pass nil to use os.Stdout.
func (rn *RaftNode) PrintStatus(w ...io.Writer) {
	var out io.Writer = os.Stdout
	if len(w) > 0 && w[0] != nil {
		out = w[0]
	}

	rn.mu.RLock()
	state := rn.state
	term := rn.currentTerm
	leaderID := rn.currentLeaderID
	rn.mu.RUnlock()

	leaderStr := "none"
	if leaderID != "" {
		leaderStr = leaderID.ShortString()
	}

	fmt.Fprintf(out, "\n=== Node Status ===\n")
	fmt.Fprintf(out, "Node ID: %s\n", rn.Transport.ID().ShortString())
	fmt.Fprintf(out, "State:   %s\n", state)
	fmt.Fprintf(out, "Term:    %d\n", term)
	fmt.Fprintf(out, "Leader:  %s\n", leaderStr)
	fmt.Fprintf(out, "Address: %s\n", rn.GetAddress())

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
	fmt.Printf("Pending tx: %d\n", len(rn.TxPool))
	for i, wrapper := range rn.TxPool {
		if wrapper.Transaction != nil {
			fmt.Printf("  %d. %s (type: %s)\n", i+1, wrapper.Transaction.GetID(), wrapper.Type)
		}
	}
	rn.TxPoolMu.Unlock()

	fmt.Fprintf(out, "\n=== Raft Log (uncommitted) ===\n")
	entries := rn.RaftLog.GetEntries()
	fmt.Fprintf(out, "Uncommitted entries: %d\n", len(entries))
	for _, e := range entries {
		fmt.Fprintf(out, "  [%d] term=%d prev=%d block=%s (%d tx)\n",
			e.Index, e.Term, e.PrevLogIndex, e.Block.BlockID, len(e.Block.Transactions))
	}

	fmt.Fprintf(out, "\n=== Ordering Blocks (committed) ===\n")
	blocks := rn.OrderingBlock.GetBlocks()
	fmt.Fprintf(out, "Committed blocks: %d\n", len(blocks))
	for i, b := range blocks {
		fmt.Printf("  Block #%d: %s (%d tx)\n", i+1, b.BlockID, len(b.Transactions))
		for j, wrapper := range b.Transactions {
			if wrapper.Transaction != nil {
				fmt.Printf("    %d. %s (type: %s)\n", j+1, wrapper.Transaction.GetID(), wrapper.Type)
			}
		}
	}
	fmt.Fprintf(out, "==================\n\n")
}
