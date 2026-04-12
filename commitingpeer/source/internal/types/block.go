package types

// Block is a cryptographically linked block of transactions,
// mirroring the Block type in the ordering service.
type Block struct {
	Timestamp    int64         `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
	PrevHash     []byte        `json:"prev_hash"`
	Hash         []byte        `json:"hash"`
	Nonce        int           `json:"nonce"`
	MerkleRoot   []byte        `json:"merkle_root"`
	Size         int           `json:"size"`
}
