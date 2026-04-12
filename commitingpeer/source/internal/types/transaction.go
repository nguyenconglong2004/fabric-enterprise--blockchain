package types

// Transaction is a Bitcoin-like UTXO transaction.
type Transaction struct {
	Version  uint32 `json:"version"`
	Vin      []VIN  `json:"vin"`
	Vout     []VOUT `json:"vout"`
	LockTime uint32 `json:"locktime"`
	Txid     string `json:"txid"`
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
