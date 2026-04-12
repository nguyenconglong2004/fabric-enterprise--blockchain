package types

// DeliverRequest is sent by this peer to the ordering service to request
// block streaming. FromIndex is 1-based and inclusive.
// 0 or 1 both mean "start from the very first block".
type DeliverRequest struct {
	FromIndex int64 `json:"from_index"`
}
