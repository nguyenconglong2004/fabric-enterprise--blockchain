package types

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// EndorsementEntry is one Ed25519 endorsement on (txid + contract_name + payload).
type EndorsementEntry struct {
	PublicKey string `json:"public_key"`
	Signature string `json:"signature"`
}

// Transaction supports both UTXO and Smart Contract transactions
type Transaction struct {
	Txid         string `json:"txid"`
	Version      uint32 `json:"version"`
	LockTime     uint32 `json:"locktime"`
	Signature    string `json:"signature"`
	ClientPubKey string `json:"client_pubkey"`
	SenderPubKey string `json:"sender_pubkey"` // Legacy: mirrors last endorser

	Endorsements []EndorsementEntry `json:"endorsements,omitempty"`

	Vin  []VIN  `json:"vin"`
	Vout []VOUT `json:"vout"`

	ContractName string `json:"contract_name"`
	FunctionName string `json:"function_name"`
	Payload      []byte `json:"payload"`
}

// UnmarshalJSON decodes payload as hex (deliver stream from order service).
func (t *Transaction) UnmarshalJSON(data []byte) error {
	type Alias struct {
		Txid           string             `json:"txid"`
		Version        uint32             `json:"version"`
		LockTime       uint32             `json:"locktime"`
		Signature      string             `json:"signature"`
		ClientPubKey   string             `json:"client_pubkey"`
		SenderPubKey   string             `json:"sender_pubkey"`
		Endorsements   []EndorsementEntry `json:"endorsements"`
		Vin            []VIN              `json:"vin"`
		Vout           []VOUT             `json:"vout"`
		ContractName   string             `json:"contract_name"`
		FunctionName   string             `json:"function_name"`
		Payload        string             `json:"payload"`
	}
	aux := &Alias{}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	t.Txid = aux.Txid
	t.Version = aux.Version
	t.LockTime = aux.LockTime
	t.Signature = aux.Signature
	t.ClientPubKey = aux.ClientPubKey
	t.SenderPubKey = aux.SenderPubKey
	t.Endorsements = aux.Endorsements
	t.Vin = aux.Vin
	t.Vout = aux.Vout
	t.ContractName = aux.ContractName
	t.FunctionName = aux.FunctionName
	if aux.Payload != "" {
		b, err := hex.DecodeString(aux.Payload)
		if err != nil {
			return fmt.Errorf("payload hex: %w", err)
		}
		t.Payload = b
	} else {
		t.Payload = nil
	}
	if len(t.Endorsements) == 0 && t.Signature != "" && t.SenderPubKey != "" {
		t.Endorsements = []EndorsementEntry{
			{PublicKey: t.SenderPubKey, Signature: t.Signature},
		}
	}
	return nil
}

// MarshalJSON encodes payload as hex.
func (t Transaction) MarshalJSON() ([]byte, error) {
	type Alias struct {
		Txid           string             `json:"txid"`
		Version        uint32             `json:"version"`
		LockTime       uint32             `json:"locktime"`
		Signature      string             `json:"signature,omitempty"`
		ClientPubKey   string             `json:"client_pubkey,omitempty"`
		SenderPubKey   string             `json:"sender_pubkey,omitempty"`
		Endorsements   []EndorsementEntry `json:"endorsements,omitempty"`
		Vin            []VIN              `json:"vin"`
		Vout           []VOUT             `json:"vout"`
		ContractName   string             `json:"contract_name"`
		FunctionName   string             `json:"function_name"`
		Payload        string             `json:"payload"`
	}
	aux := Alias{
		Txid:           t.Txid,
		Version:        t.Version,
		LockTime:       t.LockTime,
		Signature:      t.Signature,
		ClientPubKey:   t.ClientPubKey,
		SenderPubKey:   t.SenderPubKey,
		Endorsements:   t.Endorsements,
		Vin:            t.Vin,
		Vout:           t.Vout,
		ContractName:   t.ContractName,
		FunctionName:   t.FunctionName,
	}
	if len(t.Payload) > 0 {
		aux.Payload = hex.EncodeToString(t.Payload)
	}
	if len(aux.Endorsements) > 0 {
		last := aux.Endorsements[len(aux.Endorsements)-1]
		aux.SenderPubKey = last.PublicKey
		aux.Signature = last.Signature
	}
	return json.Marshal(aux)
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
