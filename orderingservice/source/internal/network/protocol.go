package network

import "time"

const (
	ProtocolID            = "/raft-order-service/1.0.0"
	DeliverProtocolID     = "/raft-order-service/deliver/1.0.0"
	EndorsementProtocolID = "/raft-order-service/endorsement/1.0.0"
	SyncProtocolID        = "/raft-order-service/sync/1.0.0"
	HeartbeatInterval     = 2 * time.Second
	HeartbeatTimeout      = 5 * time.Second
	DetectionTimeout      = 3 * time.Second

	// Sync protocol tunables
	SyncDiscoveryWindow = 2 * time.Second
	SyncFetchTimeout    = 30 * time.Second
	SyncShardSize       = 64 // số block/log entries trên mỗi shard fetch parallel
)
