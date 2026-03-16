package raft

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/libp2p/go-libp2p/core/network"

	"raft-order-service/internal/types"
)

type deliverSubscriber struct {
	ch chan types.Block
}

// DeliverManager fans out committed blocks to all connected committing peers.
type DeliverManager struct {
	mu          sync.Mutex
	subscribers map[int]*deliverSubscriber
	nextID      int
}

func NewDeliverManager() *DeliverManager {
	return &DeliverManager{
		subscribers: make(map[int]*deliverSubscriber),
	}
}

func (dm *DeliverManager) subscribe() (int, chan types.Block) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	id := dm.nextID
	dm.nextID++
	ch := make(chan types.Block, 64)
	dm.subscribers[id] = &deliverSubscriber{ch: ch}
	return id, ch
}

func (dm *DeliverManager) unsubscribe(id int) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	delete(dm.subscribers, id)
}

// NotifyNewBlock sends the block to all subscribers (non-blocking per subscriber).
func (dm *DeliverManager) NotifyNewBlock(block types.Block) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	for _, sub := range dm.subscribers {
		select {
		case sub.ch <- block:
		default:
			// subscriber is slow; drop rather than block
		}
	}
}

// HandleDeliverStream serves a long-lived stream to a committing peer.
func (rn *RaftNode) HandleDeliverStream(s network.Stream) {
	defer s.Close()

	decoder := json.NewDecoder(s)
	encoder := json.NewEncoder(s)

	var req types.DeliverRequest
	if err := decoder.Decode(&req); err != nil {
		log.Printf("[%s] deliver: failed to decode request: %v",
			rn.Transport.ID().ShortString(), err)
		return
	}

	fromIndex := req.FromIndex
	if fromIndex < 1 {
		fromIndex = 1
	}

	// Send existing blocks from fromIndex
	existingBlocks := rn.OrderingBlock.GetBlocks()
	for i := int64(0); i < int64(len(existingBlocks)); i++ {
		if i+1 >= fromIndex { // blocks are 1-indexed by position
			if err := encoder.Encode(existingBlocks[i]); err != nil {
				log.Printf("[%s] deliver: error sending existing block: %v",
					rn.Transport.ID().ShortString(), err)
				return
			}
		}
	}

	// Subscribe to future blocks
	subID, ch := rn.DeliverMgr.subscribe()
	defer rn.DeliverMgr.unsubscribe(subID)

	log.Printf("[%s] deliver: peer subscribed (sub=%d, fromIndex=%d)",
		rn.Transport.ID().ShortString(), subID, fromIndex)

	for {
		select {
		case block := <-ch:
			if err := encoder.Encode(block); err != nil {
				log.Printf("[%s] deliver: sub=%d disconnected: %v",
					rn.Transport.ID().ShortString(), subID, err)
				return
			}
		case <-rn.stopChan:
			return
		}
	}
}
