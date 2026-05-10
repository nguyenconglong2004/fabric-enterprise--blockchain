package types

// SyncDataKind phân biệt loại dữ liệu được fetch trong sync stream.
type SyncDataKind int

const (
	SyncKindBlocks SyncDataKind = iota
	SyncKindLogEntries
)

// SyncStatusRequest hỏi peer về trạng thái hiện tại để chọn sync target.
type SyncStatusRequest struct {
	RequesterCommitIndex int64 `json:"requester_commit_index"`
}

// SyncStatusResponse: peer trả lời với toàn cảnh state.
type SyncStatusResponse struct {
	Term              int64  `json:"term"`
	CommitIndex       int64  `json:"commit_index"`
	CommitHash        []byte `json:"commit_hash"`
	LogLastIndex      int64  `json:"log_last_index"`
	MembershipVersion int64  `json:"membership_version"`
	LeaderID          string `json:"leader_id"`
}

// SyncDataRequest: gửi qua libp2p stream để fetch một range.
// FromIndex và ToIndex đều inclusive, 1-based.
type SyncDataRequest struct {
	Kind      SyncDataKind `json:"kind"`
	FromIndex int64        `json:"from_index"`
	ToIndex   int64        `json:"to_index"`
}

// SyncDataChunk: server stream từng chunk về client.
type SyncDataChunk struct {
	Kind    SyncDataKind `json:"kind"`
	Blocks  []Block      `json:"blocks,omitempty"`  // dùng khi Kind = SyncKindBlocks
	Entries []LogEntry   `json:"entries,omitempty"` // dùng khi Kind = SyncKindLogEntries
	EOF     bool         `json:"eof"`
	Err     string       `json:"err,omitempty"`
}
