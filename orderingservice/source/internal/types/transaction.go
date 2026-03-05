package types

import "time"

// Transaction represents a client transaction submitted to the ordering service
type Transaction struct {
	ID        string
	Data      string
	Timestamp time.Time
}
