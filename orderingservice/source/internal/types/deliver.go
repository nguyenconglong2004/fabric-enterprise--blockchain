package types

// DeliverRequest is sent by a committing peer to request block streaming.
// FromIndex is 1-based and inclusive. 0 or 1 means start from the beginning.
type DeliverRequest struct {
	FromIndex int64
}
