// File: internal/core/models.go
package core

type Transaction struct {
	TxID         string `json:"tx_id"`
	SenderPubKey string `json:"sender_pubkey"`
	Signature    string `json:"signature"`

	ContractName string `json:"contract_name"`
	FunctionName string `json:"function_name"`

	Payload []byte `json:"payload"`
}

type Block struct {
	BlockHeight int64         `json:"block_height"`
	PrevHash    string        `json:"prev_hash"`
	BlockHash   string        `json:"block_hash"`
	Txs         []Transaction `json:"txs"`
}
