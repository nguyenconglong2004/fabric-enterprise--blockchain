package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
)

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

// Serialize returns the binary encoding of the transaction (Bitcoin wire format).
func (tx *Transaction) Serialize() []byte {
	buf := new(bytes.Buffer)

	binary.Write(buf, binary.LittleEndian, tx.Version)

	writeVarInt(buf, uint64(len(tx.Vin)))
	for _, vin := range tx.Vin {
		prevBytes, _ := hexToBytesFixed32(vin.Txid)
		buf.Write(reverseBytes(prevBytes))
		binary.Write(buf, binary.LittleEndian, uint32(vin.Vout))
		script, _ := hex.DecodeString(vin.ScriptSig.Hex)
		writeVarInt(buf, uint64(len(script)))
		buf.Write(script)
		binary.Write(buf, binary.LittleEndian, uint32(0xffffffff))
	}

	writeVarInt(buf, uint64(len(tx.Vout)))
	for _, vout := range tx.Vout {
		binary.Write(buf, binary.LittleEndian, uint64(vout.Value))
		scriptBytes, _ := hex.DecodeString(vout.ScriptPubKey.Hex)
		writeVarInt(buf, uint64(len(scriptBytes)))
		buf.Write(scriptBytes)
	}

	binary.Write(buf, binary.LittleEndian, tx.LockTime)
	return buf.Bytes()
}

// ComputeTxID computes the double-SHA256 of the serialized transaction.
func (tx *Transaction) ComputeTxID() string {
	raw := tx.Serialize()
	h1 := sha256.Sum256(raw)
	h2 := sha256.Sum256(h1[:])
	return hex.EncodeToString(reverseBytes(h2[:]))
}

// Size returns the byte length of the serialized transaction.
func (tx *Transaction) Size() int {
	return len(tx.Serialize())
}

// ShallowCopyEmptySigs returns a copy of the transaction with all ScriptSig fields cleared.
func (tx *Transaction) ShallowCopyEmptySigs() Transaction {
	newVin := make([]VIN, len(tx.Vin))
	for i := range tx.Vin {
		newVin[i] = VIN{
			Txid: tx.Vin[i].Txid,
			Vout: tx.Vin[i].Vout,
		}
	}
	newVout := make([]VOUT, len(tx.Vout))
	copy(newVout, tx.Vout)
	return Transaction{
		Version:  tx.Version,
		Vin:      newVin,
		Vout:     newVout,
		LockTime: tx.LockTime,
	}
}

// Validate performs basic sanity checks on the transaction.
func (tx *Transaction) Validate() error {
	if tx.Txid == "" {
		return errors.New("transaction must have a valid txid")
	}
	if len(tx.Vin) == 0 {
		return errors.New("transaction must have at least one input")
	}
	if len(tx.Vout) == 0 {
		return errors.New("transaction must have at least one output")
	}
	return nil
}

// --- internal helpers (not exported) ---

func writeVarInt(buf *bytes.Buffer, n uint64) {
	switch {
	case n < 0xfd:
		buf.WriteByte(byte(n))
	case n <= 0xffff:
		buf.WriteByte(0xfd)
		binary.Write(buf, binary.LittleEndian, uint16(n))
	case n <= 0xffffffff:
		buf.WriteByte(0xfe)
		binary.Write(buf, binary.LittleEndian, uint32(n))
	default:
		buf.WriteByte(0xff)
		binary.Write(buf, binary.LittleEndian, n)
	}
}

func hexToBytesFixed32(hexStr string) ([]byte, error) {
	raw, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, err
	}
	if len(raw) > 32 {
		return nil, errors.New("hex string too long for 32-byte field")
	}
	padded := make([]byte, 32)
	copy(padded[32-len(raw):], raw)
	return padded, nil
}

func reverseBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[i] = b[len(b)-1-i]
	}
	return out
}
