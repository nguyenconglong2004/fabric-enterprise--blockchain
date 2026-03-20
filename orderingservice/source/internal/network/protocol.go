package network

import "time"

const (
	ProtocolID        = "/raft-order-service/1.0.0"
	DeliverProtocolID = "/raft-order-service/deliver/1.0.0"
	HeartbeatInterval = 2 * time.Second
	HeartbeatTimeout  = 5 * time.Second
	DetectionTimeout  = 3 * time.Second
)
