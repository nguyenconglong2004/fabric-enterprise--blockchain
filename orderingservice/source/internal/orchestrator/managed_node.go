package orchestrator

import (
	"context"
	"log"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"raft-order-service/internal/raft"
	"raft-order-service/internal/types"
)

// ManagedNode wraps a RaftNode with orchestration metadata.
type ManagedNode struct {
	Port   int
	Raft   *raft.RaftNode
	Cancel context.CancelFunc
	Logger *log.Logger
}

// BusEmitter implements raft.EventEmitter and pushes structured events to the EventBus.
// peerPortFn resolves a peer.ID to its TCP port within the managed cluster.
type BusEmitter struct {
	port       int
	bus        *EventBus
	peerPortFn func(peer.ID) int
}

func NewBusEmitter(port int, bus *EventBus, peerPortFn func(peer.ID) int) *BusEmitter {
	return &BusEmitter{port: port, bus: bus, peerPortFn: peerPortFn}
}

func (e *BusEmitter) HeartbeatSent(from, to peer.ID, term int64) {
	e.bus.Publish(MakeEvent("heartbeat-sent", map[string]interface{}{
		"fromPort": e.port,
		"toPort":   e.peerPortFn(to),
		"term":     term,
		"ts":       time.Now().UnixMilli(),
	}))
}

func (e *BusEmitter) HeartbeatReceived(self, from peer.ID) {
	e.bus.Publish(MakeEvent("heartbeat-received", map[string]interface{}{
		"port":     e.port,
		"fromPort": e.peerPortFn(from),
		"ts":       time.Now().UnixMilli(),
	}))
}

func (e *BusEmitter) StateChanged(self peer.ID, from, to types.NodeState) {
	e.bus.Publish(MakeEvent("state-changed", map[string]interface{}{
		"port": e.port,
		"from": from.String(),
		"to":   to.String(),
	}))
}

func (e *BusEmitter) TermChanged(self peer.ID, term int64) {
	e.bus.Publish(MakeEvent("term-changed", map[string]interface{}{
		"port": e.port,
		"term": term,
	}))
}

func (e *BusEmitter) LeaderClaimStarted(self peer.ID, term int64) {
	e.bus.Publish(MakeEvent("leader-claim", map[string]interface{}{
		"port": e.port,
		"term": term,
	}))
}

func (e *BusEmitter) LeaderClaimAck(from, to peer.ID, accept bool) {
	e.bus.Publish(MakeEvent("claim-ack", map[string]interface{}{
		"fromPort": e.peerPortFn(from),
		"toPort":   e.peerPortFn(to),
		"accept":   accept,
	}))
}

func (e *BusEmitter) BecameLeader(self peer.ID, term int64) {
	e.bus.Publish(MakeEvent("became-leader", map[string]interface{}{
		"port": e.port,
		"term": term,
	}))
}

func (e *BusEmitter) BlockProposed(leader peer.ID, blockHash string, txCount int) {
	e.bus.Publish(MakeEvent("block-proposed", map[string]interface{}{
		"port":    e.port,
		"hash":    blockHash,
		"txCount": txCount,
	}))
}

func (e *BusEmitter) BlockCommitted(self peer.ID, blockIndex uint64, blockHash string, txCount int) {
	e.bus.Publish(MakeEvent("block-committed", map[string]interface{}{
		"port":       e.port,
		"blockIndex": blockIndex,
		"hash":       blockHash,
		"txCount":    txCount,
	}))
}

func (e *BusEmitter) MembershipChanged(self peer.ID, version int64) {
	e.bus.Publish(MakeEvent("membership-version", map[string]interface{}{
		"port":    e.port,
		"version": version,
	}))
}

func (e *BusEmitter) TxPoolChanged(leader peer.ID, size int) {
	e.bus.Publish(MakeEvent("tx-pool", map[string]interface{}{
		"port": e.port,
		"size": size,
	}))
}
