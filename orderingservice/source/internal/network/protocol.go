package network

import "time"

const (
	ProtocolID            = "/raft-order-service/1.0.0"
	DeliverProtocolID     = "/raft-order-service/deliver/1.0.0"
	EndorsementProtocolID = "/raft-order-service/endorsement/1.0.0"
	TransactionProtocolID = "/raft-order-service/transaction/1.0.0"
	MembershipProtocolID  = "/raft-order-service/membership/1.0.0"
	HeartbeatInterval     = 2 * time.Second
	HeartbeatTimeout      = 5 * time.Second
	DetectionTimeout      = 3 * time.Second
)
