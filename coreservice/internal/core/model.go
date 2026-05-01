// File: internal/core/models.go
package core

import (
	"encoding/hex"
	"encoding/json"
)

type VIN struct {
	Txid      string    `json:"txid"`
	Vout      int       `json:"vout"`
	ScriptSig ScriptSig `json:"scriptSig"`
}

type ScriptSig struct {
	ASM string `json:"asm"`
	Hex string `json:"hex"`
}

type VOUT struct {
	Value        int64        `json:"value"`
	N            int          `json:"n"`
	ScriptPubKey ScriptPubKey `json:"scriptPubKey"`
}

type ScriptPubKey struct {
	ASM       string   `json:"asm"`
	Hex       string   `json:"hex"`
	Addresses []string `json:"addresses"`
}

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
	Payload      []byte `json:"-"` // Don't serialize as JSON
}

// Custom JSON unmarshaler to handle payload conversion from hex string
func (t *Transaction) UnmarshalJSON(data []byte) error {
	type Alias struct {
		Txid         string `json:"txid"`
		Version      uint32 `json:"version"`
		LockTime     uint32 `json:"locktime"`
		Signature    string `json:"signature"`
		ClientPubKey string `json:"client_pubkey"`
		SenderPubKey string `json:"sender_pubkey"`
		Vin          []VIN  `json:"vin"`
		Vout         []VOUT `json:"vout"`
		ContractName string `json:"contract_name"`
		FunctionName string `json:"function_name"`
		Payload      string `json:"payload"` // Hex string
	}

	aux := &Alias{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	t.Txid = aux.Txid
	t.Version = aux.Version
	t.LockTime = aux.LockTime
	t.Signature = aux.Signature
	t.ClientPubKey = aux.ClientPubKey
	t.SenderPubKey = aux.SenderPubKey
	t.Vin = aux.Vin
	t.Vout = aux.Vout
	t.ContractName = aux.ContractName
	t.FunctionName = aux.FunctionName

	// Convert hex string to bytes
	if aux.Payload != "" {
		payload, err := hex.DecodeString(aux.Payload)
		if err != nil {
			return err
		}
		t.Payload = payload
	}
	return nil
}

// Custom JSON marshaler to handle payload conversion to hex string
func (t Transaction) MarshalJSON() ([]byte, error) {
	type Alias struct {
		Txid         string `json:"txid"`
		Version      uint32 `json:"version"`
		LockTime     uint32 `json:"locktime"`
		Signature    string `json:"signature"`
		ClientPubKey string `json:"client_pubkey"`
		SenderPubKey string `json:"sender_pubkey"`
		Vin          []VIN  `json:"vin"`
		Vout         []VOUT `json:"vout"`
		ContractName string `json:"contract_name"`
		FunctionName string `json:"function_name"`
		Payload      string `json:"payload"`
	}

	aux := Alias{
		Txid:         t.Txid,
		Version:      t.Version,
		LockTime:     t.LockTime,
		Signature:    t.Signature,
		ClientPubKey: t.ClientPubKey,
		SenderPubKey: t.SenderPubKey,
		Vin:          t.Vin,
		Vout:         t.Vout,
		ContractName: t.ContractName,
		FunctionName: t.FunctionName,
		Payload:      hex.EncodeToString(t.Payload),
	}

	return json.Marshal(aux)
}

type Block struct {
	BlockHeight int64         `json:"block_height"`
	PrevHash    string        `json:"prev_hash"`
	BlockHash   string        `json:"block_hash"`
	Txs         []Transaction `json:"txs"`
}
