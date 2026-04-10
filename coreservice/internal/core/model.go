// File: internal/core/models.go
package core

import "encoding/json"

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

// RWSet chứa lịch sử Đọc/Ghi của Smart Contract trong quá trình chạy nháp
type RWSet struct {
	ReadSet  map[string]string `json:"read_set"`  // Lưu lại Key và giá trị (hoặc version) lúc đọc
	WriteSet map[string][]byte `json:"write_set"` // Lưu lại Key và dữ liệu mới muốn ghi
}

// TransactionProposal là gói tin Client gửi lên (Chưa có chữ ký Endorser)
type TransactionProposal struct {
	TxID         string          `json:"tx_id"`
	ContractName string          `json:"contract_name"`
	FunctionName string          `json:"function_name"`
	Payload      json.RawMessage `json:"payload"`
	ClientPubKey string          `json:"client_pubkey"`
	Signature    string          `json:"signature"`
}

// EndorsementResponse là gói tin Node trả về cho Client sau khi chạy nháp xong
type EndorsementResponse struct {
	TxID       string `json:"tx_id"`
	EndorserID string `json:"endorser_id"` // Tên của Node (VD: Org1Peer1)
	RWSet      RWSet  `json:"rw_set"`
	Status     int    `json:"status"` // 200 là hợp lệ, 500 là lỗi logic
	Message    string `json:"message"`
	Signature  string `json:"signature"` // Chữ ký của Peer xác nhận RWSet này
}
