package raft

import (
	"github.com/libp2p/go-libp2p/core/peer"

	"raft-order-service/internal/types"
)

// EventEmitter is the interface for emitting Raft node events to external observers (e.g. the web UI orchestrator).
// All implementations must be non-blocking; drop the event if the subscriber is slow.
type EventEmitter interface {
	HeartbeatSent(from, to peer.ID, term int64)
	HeartbeatReceived(self, from peer.ID)
	StateChanged(self peer.ID, from, to types.NodeState)
	TermChanged(self peer.ID, term int64)
	LeaderClaimStarted(self peer.ID, term int64)
	LeaderClaimAck(from, to peer.ID, accept bool)
	BecameLeader(self peer.ID, term int64)
	BlockProposed(leader peer.ID, blockHash string, txCount int)
	BlockCommitted(self peer.ID, blockIndex uint64, blockHash string, txCount int)
	MembershipChanged(self peer.ID, version int64)
	TxPoolChanged(leader peer.ID, size int)
}

// NoopEmitter is a no-op EventEmitter used by the CLI server.
type NoopEmitter struct{}

func (NoopEmitter) HeartbeatSent(from, to peer.ID, term int64)            {}
func (NoopEmitter) HeartbeatReceived(self, from peer.ID)                  {}
func (NoopEmitter) StateChanged(self peer.ID, from, to types.NodeState)   {}
func (NoopEmitter) TermChanged(self peer.ID, term int64)                  {}
func (NoopEmitter) LeaderClaimStarted(self peer.ID, term int64)           {}
func (NoopEmitter) LeaderClaimAck(from, to peer.ID, accept bool)          {}
func (NoopEmitter) BecameLeader(self peer.ID, term int64)                 {}
func (NoopEmitter) BlockProposed(leader peer.ID, blockHash string, txCount int) {}
func (NoopEmitter) BlockCommitted(self peer.ID, blockIndex uint64, blockHash string, txCount int) {}
func (NoopEmitter) MembershipChanged(self peer.ID, version int64)         {}
func (NoopEmitter) TxPoolChanged(leader peer.ID, size int)                {}
