package raft

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"raft-order-service/internal/types"
)

// SubmitOrder submits a new order to the service
func (rn *RaftNode) SubmitOrder(orderData string) (string, error) {
	// If we're not the leader, forward to leader
	if !rn.IsLeader() {
		leaderID := rn.GetLeaderID()
		if leaderID == "" {
			return "", fmt.Errorf("no leader available")
		}
		return rn.forwardOrderToLeader(orderData, leaderID)
	}

	// We are the leader - process the order
	return rn.processOrder(orderData)
}

// processOrder processes an order (leader only)
func (rn *RaftNode) processOrder(orderData string) (string, error) {
	orderID := fmt.Sprintf("order-%d", time.Now().UnixNano())

	order := types.Order{
		ID:        orderID,
		Data:      orderData,
		Timestamp: time.Now(),
		Status:    "pending",
	}

	log.Printf("[%s] Processing order: %s", rn.Transport.ID().ShortString(), orderID)

	// In a real implementation, you would:
	// 1. Replicate to followers
	// 2. Wait for majority acknowledgment
	// 3. Commit to log

	// For simplicity, we'll just commit directly
	order.Status = "committed"
	rn.OrderLog.AppendOrder(order)

	log.Printf("[%s] Order committed: %s", rn.Transport.ID().ShortString(), orderID)

	// Replicate to followers
	rn.replicateOrder(order)

	return orderID, nil
}

// replicateOrder replicates an order to all followers
func (rn *RaftNode) replicateOrder(order types.Order) {
	msg := types.Message{
		Type:      types.MsgOrderRequest,
		Term:      rn.currentTerm,
		SenderID:  rn.Transport.ID().String(),
		Data:      order,
		Timestamp: time.Now(),
	}

	rn.BroadcastMessage(msg)
}

// forwardOrderToLeader forwards an order request to the leader
func (rn *RaftNode) forwardOrderToLeader(orderData string, leaderID peer.ID) (string, error) {
	log.Printf("[%s] Forwarding order to leader %s",
		rn.Transport.ID().ShortString(), leaderID.ShortString())

	order := types.Order{
		ID:        fmt.Sprintf("order-%d", time.Now().UnixNano()),
		Data:      orderData,
		Timestamp: time.Now(),
		Status:    "pending",
	}

	msg := types.Message{
		Type:      types.MsgOrderRequest,
		Term:      rn.currentTerm,
		SenderID:  rn.Transport.ID().String(),
		Data:      order,
		Timestamp: time.Now(),
	}

	if err := rn.Transport.SendMessage(leaderID, msg); err != nil {
		return "", fmt.Errorf("failed to forward to leader: %w", err)
	}

	// In a real implementation, you would wait for a response
	// For simplicity, we'll return the order ID immediately
	return order.ID, nil
}

// HandleOrderRequest handles order requests from clients or followers
func (rn *RaftNode) HandleOrderRequest(msg types.Message) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		log.Printf("[%s] Error marshaling order data: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	var order types.Order
	if err := json.Unmarshal(data, &order); err != nil {
		log.Printf("[%s] Error unmarshaling order: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	// Check if sender is a member of the cluster
	senderID, err := peer.Decode(msg.SenderID)
	if err != nil {
		return
	}

	rn.Membership.Mu.RLock()
	_, isMember := rn.Membership.Members[senderID]
	rn.Membership.Mu.RUnlock()

	rn.mu.RLock()
	currentTerm := rn.currentTerm
	leaderID := rn.currentLeaderID
	rn.mu.RUnlock()

	// If message is from leader and term is valid, it's a replication
	if senderID == leaderID && msg.Term >= currentTerm {
		// This is a replication from leader - check if we have pending order with same ID
		existingOrder := rn.OrderLog.FindOrderByID(order.ID)
		if existingOrder != nil && existingOrder.Status == "pending" {
			// Update existing pending order to committed
			rn.OrderLog.UpdateOrderStatus(order.ID, "committed")
			log.Printf("[%s] Updated order %s from pending to committed", rn.Transport.ID().ShortString(), order.ID)
		} else if existingOrder == nil {
			// New order from leader, commit directly
			order.Status = "committed"
			rn.OrderLog.AppendOrder(order)
			log.Printf("[%s] Replicating order: %s", rn.Transport.ID().ShortString(), order.ID)
		}
	} else if !isMember {
		// Sender is not a member (client) - store as pending
		// Check if order already exists
		existingOrder := rn.OrderLog.FindOrderByID(order.ID)
		if existingOrder == nil {
			// New order from client, store as pending
			order.Status = "pending"
			rn.OrderLog.AppendOrder(order)
			log.Printf("[%s] Received order from client: %s (status: pending)", rn.Transport.ID().ShortString(), order.ID)
		}

		// Leader also stores as pending - commit will be done manually
		// No automatic commit and replication
	}
}

// HandleOrderResponse handles order responses
func (rn *RaftNode) HandleOrderResponse(msg types.Message) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}

	var order types.Order
	if err := json.Unmarshal(data, &order); err != nil {
		return
	}

	log.Printf("[%s] Order response received: %s (status: %s)",
		rn.Transport.ID().ShortString(), order.ID, order.Status)
}

// GetOrders returns all committed orders
func (rn *RaftNode) GetOrders() []types.Order {
	return rn.OrderLog.GetOrders()
}

// ProposeBlock proposes a block containing multiple transactions (orders)
func (rn *RaftNode) ProposeBlock(indices []int) error {
	if !rn.IsLeader() {
		return fmt.Errorf("only leader can propose blocks")
	}

	orders := rn.OrderLog.GetOrders()

	// Validate indices and collect orders
	blockOrders := make([]types.Order, 0, len(indices))
	for _, idx := range indices {
		if idx < 1 || idx > len(orders) {
			return fmt.Errorf("invalid order index: %d (total orders: %d)", idx, len(orders))
		}

		order := orders[idx-1] // Convert to 0-based index

		if order.Status == "committed" {
			return fmt.Errorf("order at index %d (%s) is already committed", idx, order.ID)
		}

		if order.Status != "pending" {
			return fmt.Errorf("order at index %d (%s) has invalid status: %s", idx, order.ID, order.Status)
		}

		blockOrders = append(blockOrders, order)
	}

	if len(blockOrders) == 0 {
		return fmt.Errorf("no valid orders to propose")
	}

	// Create block
	rn.mu.RLock()
	currentTerm := rn.currentTerm
	rn.mu.RUnlock()

	block := types.Block{
		BlockID:    fmt.Sprintf("block-%d", time.Now().UnixNano()),
		Orders:     blockOrders,
		Timestamp:  time.Now(),
		ProposerID: rn.Transport.ID().String(),
		Term:       currentTerm,
	}

	log.Printf("[%s] Proposing block %s with %d transaction(s)",
		rn.Transport.ID().ShortString(), block.BlockID, len(blockOrders))

	// Send proposal to all followers
	proposal := types.BlockProposal{
		Block:     block,
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

	// Wait for ACKs from majority
	go rn.waitForBlockAcks(block)

	return nil
}

// waitForBlockAcks waits for acknowledgments from followers for a block proposal
func (rn *RaftNode) waitForBlockAcks(block types.Block) {
	acks := make(map[peer.ID]bool)
	acks[rn.Transport.ID()] = true // Count ourselves

	aliveCount := len(rn.Membership.GetAliveMembers())
	majority := aliveCount/2 + 1

	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	log.Printf("[%s] Waiting for block proposal ACKs (need %d/%d)",
		rn.Transport.ID().ShortString(), majority, aliveCount)

	for {
		select {
		case <-timeout:
			ackCount := len(acks)
			if ackCount >= majority {
				log.Printf("[%s] Received majority ACKs (%d/%d) for block %s",
					rn.Transport.ID().ShortString(), ackCount, majority, block.BlockID)
				rn.commitBlock(block)
			} else {
				log.Printf("[%s] Failed to get majority ACKs (%d/%d) for block %s",
					rn.Transport.ID().ShortString(), ackCount, majority, block.BlockID)
			}
			return

		case <-ticker.C:
			ackCount := len(acks)
			if ackCount >= majority {
				log.Printf("[%s] Received majority ACKs (%d/%d) for block %s",
					rn.Transport.ID().ShortString(), ackCount, majority, block.BlockID)
				rn.commitBlock(block)
				return
			}

		case msg := <-rn.BlockAckChan:
			// Check if this ACK is for our block
			data, err := json.Marshal(msg.Data)
			if err != nil {
				continue
			}

			var ack types.BlockProposalAck
			if err := json.Unmarshal(data, &ack); err != nil {
				continue
			}

			if ack.BlockID == block.BlockID && ack.Accepted {
				senderID, err := peer.Decode(msg.SenderID)
				if err == nil {
					acks[senderID] = true
					log.Printf("[%s] Received ACK from %s for block %s (%d total)",
						rn.Transport.ID().ShortString(), senderID.ShortString(), block.BlockID, len(acks))
				}
			}
		}
	}
}

// commitBlock commits a block after receiving majority ACKs
func (rn *RaftNode) commitBlock(block types.Block) {
	log.Printf("[%s] Committing block %s with %d transaction(s)",
		rn.Transport.ID().ShortString(), block.BlockID, len(block.Orders))

	// Update status of all orders in block to committed
	for _, order := range block.Orders {
		rn.OrderLog.UpdateOrderStatus(order.ID, "committed")
		log.Printf("[%s] Committed order %s in block %s",
			rn.Transport.ID().ShortString(), order.ID, block.BlockID)
	}

	// Send block to external system (placeholder - just log for now)
	log.Printf("[%s] Sending block %s to external system (placeholder)",
		rn.Transport.ID().ShortString(), block.BlockID)

	// Collect order IDs for commit message
	orderIDs := make([]string, len(block.Orders))
	for i, order := range block.Orders {
		orderIDs[i] = order.ID
	}

	// Send commit notification to followers
	commit := types.BlockCommit{
		BlockID:   block.BlockID,
		OrderIDs:  orderIDs,
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

	log.Printf("[%s] Block %s committed and notified followers",
		rn.Transport.ID().ShortString(), block.BlockID)

	// Signal auto-propose goroutine (non-blocking)
	select {
	case rn.blockCommittedNotify <- struct{}{}:
	default:
	}
}

// HandleBlockProposal handles block proposal from leader
func (rn *RaftNode) HandleBlockProposal(msg types.Message) {
	if rn.IsLeader() {
		// Leaders don't process proposals from other leaders
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

	block := proposal.Block

	// Verify the proposal
	rn.mu.RLock()
	currentTerm := rn.currentTerm
	leaderID := rn.currentLeaderID
	rn.mu.RUnlock()

	// Check if message is from current leader
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
		rn.sendBlockProposalAck(block.BlockID, false, "old term")
		return
	}

	// Verify all transactions in the block
	allValid := true
	reason := ""

	for _, order := range block.Orders {
		// Check if order exists in our log
		existingOrder := rn.OrderLog.FindOrderByID(order.ID)
		if existingOrder == nil {
			allValid = false
			reason = fmt.Sprintf("order %s not found", order.ID)
			break
		}

		// Check if order is pending
		if existingOrder.Status != "pending" {
			allValid = false
			reason = fmt.Sprintf("order %s has status %s (expected pending)", order.ID, existingOrder.Status)
			break
		}

		// Verify order data matches
		if existingOrder.Data != order.Data {
			allValid = false
			reason = fmt.Sprintf("order %s data mismatch", order.ID)
			break
		}
	}

	if allValid {
		log.Printf("[%s] Block proposal %s verified successfully (%d transactions)",
			rn.Transport.ID().ShortString(), block.BlockID, len(block.Orders))
		rn.sendBlockProposalAck(block.BlockID, true, "")
	} else {
		log.Printf("[%s] Block proposal %s verification failed: %s",
			rn.Transport.ID().ShortString(), block.BlockID, reason)
		rn.sendBlockProposalAck(block.BlockID, false, reason)
	}
}

// sendBlockProposalAck sends acknowledgment for a block proposal
func (rn *RaftNode) sendBlockProposalAck(blockID string, accepted bool, reason string) {
	ack := types.BlockProposalAck{
		BlockID:   blockID,
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

	// Forward to blockAckChan for waitForBlockAcks to process
	select {
	case rn.BlockAckChan <- msg:
	default:
		// Channel full, log warning
		log.Printf("[%s] Block ACK channel full, dropping ACK", rn.Transport.ID().ShortString())
	}
}

// HandleBlockCommit handles block commit notification from leader
func (rn *RaftNode) HandleBlockCommit(msg types.Message) {
	if rn.IsLeader() {
		// Leaders don't process commits from other leaders
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

	// Verify message is from current leader
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

	// Verify term
	if msg.Term < currentTerm {
		log.Printf("[%s] Block commit with old term %d (current: %d), ignoring",
			rn.Transport.ID().ShortString(), msg.Term, currentTerm)
		return
	}

	log.Printf("[%s] Received block commit notification for block %s with %d order(s)",
		rn.Transport.ID().ShortString(), commit.BlockID, len(commit.OrderIDs))

	// Commit all orders in the block
	committedCount := 0
	for _, orderID := range commit.OrderIDs {
		existingOrder := rn.OrderLog.FindOrderByID(orderID)
		if existingOrder != nil && existingOrder.Status == "pending" {
			rn.OrderLog.UpdateOrderStatus(orderID, "committed")
			committedCount++
			log.Printf("[%s] Committed order %s in block %s",
				rn.Transport.ID().ShortString(), orderID, commit.BlockID)
		} else if existingOrder == nil {
			log.Printf("[%s] Warning: Order %s in block %s not found in log",
				rn.Transport.ID().ShortString(), orderID, commit.BlockID)
		} else if existingOrder.Status != "pending" {
			log.Printf("[%s] Warning: Order %s in block %s has status %s (expected pending)",
				rn.Transport.ID().ShortString(), orderID, commit.BlockID, existingOrder.Status)
		}
	}

	log.Printf("[%s] Block %s commit complete: %d/%d orders committed",
		rn.Transport.ID().ShortString(), commit.BlockID, committedCount, len(commit.OrderIDs))
}

// StartAutoProposeBlock starts an auto-propose loop on the leader.
// It continuously groups up to batchSize pending transactions into a block,
// proposes it, waits for commit confirmation, then processes the next batch
// until no pending transactions remain. The loop keeps watching for new arrivals.
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
			log.Printf("[%s] Auto-propose goroutine exited",
				rn.Transport.ID().ShortString())
		}()

		for {
			// Check stop signal
			select {
			case <-stopChan:
				return
			default:
			}

			// Stop if no longer leader
			if !rn.IsLeader() {
				log.Printf("[%s] Auto-propose: stepped down from leader, stopping",
					rn.Transport.ID().ShortString())
				return
			}

			// Find next batch of pending transactions
			indices := rn.OrderLog.GetPendingOrderIndices(batchSize)
			if len(indices) == 0 {
				// No pending orders — wait briefly then recheck
				select {
				case <-stopChan:
					return
				case <-time.After(200 * time.Millisecond):
				}
				continue
			}

			log.Printf("[%s] Auto-propose: proposing block with %d tx (indices %v)",
				rn.Transport.ID().ShortString(), len(indices), indices)

			if err := rn.ProposeBlock(indices); err != nil {
				log.Printf("[%s] Auto-propose: ProposeBlock error: %v",
					rn.Transport.ID().ShortString(), err)
				select {
				case <-stopChan:
					return
				case <-time.After(500 * time.Millisecond):
				}
				continue
			}

			// Wait for the block commit confirmation before proposing the next batch
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
	rn.autoProposeStop = nil // prevent double-close
	log.Printf("[%s] Auto-propose stop signal sent", rn.Transport.ID().ShortString())
}

// IsAutoProposeRunning returns true if the auto-propose loop is active.
func (rn *RaftNode) IsAutoProposeRunning() bool {
	rn.autoProposeMu.Lock()
	defer rn.autoProposeMu.Unlock()
	return rn.autoProposeRunning
}

// PrintStatus prints the current status of the node
func (rn *RaftNode) PrintStatus() {
	rn.mu.RLock()
	state := rn.state
	term := rn.currentTerm
	leaderID := rn.currentLeaderID
	rn.mu.RUnlock()

	leaderStr := "none"
	if leaderID != "" {
		leaderStr = leaderID.ShortString()
	}

	fmt.Printf("\n=== Node Status ===\n")
	fmt.Printf("Node ID: %s\n", rn.Transport.ID().ShortString())
	fmt.Printf("State: %s\n", state)
	fmt.Printf("Term: %d\n", term)
	fmt.Printf("Leader: %s\n", leaderStr)
	fmt.Printf("Address: %s\n", rn.GetAddress())

	fmt.Printf("\n=== Membership ===\n")
	members := rn.Membership.GetAliveMembers()
	fmt.Printf("Total alive members: %d\n", len(members))
	for _, member := range members {
		isLeader := ""
		if member.PeerID == leaderID {
			isLeader = " (LEADER)"
		}
		isSelf := ""
		if member.PeerID == rn.Transport.ID() {
			isSelf = " (SELF)"
		}
		fmt.Printf("  - Priority %d: %s%s%s (joined: %s)\n",
			member.Priority,
			member.PeerID.ShortString(),
			isLeader,
			isSelf,
			member.JoinTime.Format("15:04:05"))
	}

	fmt.Printf("\n=== Orders ===\n")
	orders := rn.GetOrders()
	fmt.Printf("Total orders: %d\n", len(orders))
	for i, order := range orders {
		fmt.Printf("  %d. %s: %s (status: %s, time: %s)\n",
			i+1,
			order.ID,
			order.Data,
			order.Status,
			order.Timestamp.Format("15:04:05"))
	}
	fmt.Printf("==================\n\n")
}
