package types

// Transaction supports both UTXO and Smart Contract transactions
type Transaction struct {
	// Common fields
	Txid         string `json:"txid"`
	Version      uint32 `json:"version"`
	LockTime     uint32 `json:"locktime"`
	Signature    string `json:"signature"`
	ClientPubKey string `json:"client_pubkey"`
	SenderPubKey string `json:"sender_pubkey"` // Endorser public key

	// UTXO transaction fields
	Vin  []VIN  `json:"vin"`
	Vout []VOUT `json:"vout"`

	// Smart Contract transaction fields
	ContractName string `json:"contract_name"`
	FunctionName string `json:"function_name"`
	Payload      []byte `json:"payload"`
}

// VIN is a transaction input referencing a previous output.
type VIN struct {
	Txid      string    `json:"txid"`
	Vout      int       `json:"vout"`
	ScriptSig ScriptSig `json:"scriptSig"`
}

// ScriptSig unlocks a previous output.
type ScriptSig struct {
	ASM string `json:"asm"`
	Hex string `json:"hex"`
}

// VOUT is a transaction output.
type VOUT struct {
	Value        int64        `json:"value"`
	N            int          `json:"n"`
	ScriptPubKey ScriptPubKey `json:"scriptPubKey"`
}

// ScriptPubKey locks an output to an address.
type ScriptPubKey struct {
	ASM       string   `json:"asm"`
	Hex       string   `json:"hex"`
	Addresses []string `json:"addresses"`
}
