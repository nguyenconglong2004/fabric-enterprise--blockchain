package types

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
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

// --- Ed25519 signing and transaction creation ---

// SignEd25519 signs each input of the transaction using Ed25519.
// prevOuts[i] must be the VOUT that Vin[i] is spending; its ScriptPubKey.Hex is
// injected into the sighash computation (matching the blockchain/ signing protocol).
// After signing all inputs, Txid is recomputed.
func (t *Transaction) SignEd25519(priv ed25519.PrivateKey, prevOuts []VOUT) error {
	if len(prevOuts) != len(t.Vin) {
		return fmt.Errorf("prevOuts length (%d) must match Vin length (%d)", len(prevOuts), len(t.Vin))
	}

	pub := priv.Public().(ed25519.PublicKey)

	for i := range t.Vin {
		// Create a copy with all ScriptSig fields cleared.
		txCopy := t.ShallowCopyEmptySigs()
		// Inject the previous output's ScriptPubKey into this input's ScriptSig for hashing.
		txCopy.Vin[i].ScriptSig.Hex = prevOuts[i].ScriptPubKey.Hex

		// Double SHA256 of the serialized copy (sighash).
		raw := txCopy.Serialize()
		h1 := sha256.Sum256(raw)
		h2 := sha256.Sum256(h1[:])

		// Sign with Ed25519 → 64-byte signature.
		sig := ed25519.Sign(priv, h2[:])

		// ScriptSig = sig(64 bytes) || pubkey(32 bytes) = 96 bytes total.
		script := append(sig, pub...)
		t.Vin[i].ScriptSig.Hex = hex.EncodeToString(script)
		t.Vin[i].ScriptSig.ASM = fmt.Sprintf("%x %x", sig, pub)
	}

	// Recompute Txid with signatures in place.
	t.Txid = t.ComputeTxID()
	return nil
}

// ClientUTXO represents an unspent output available for spending by the client.
type ClientUTXO struct {
	Txid    string
	VoutIdx int
	Out     VOUT
}

// CreateTransaction creates and signs a new UTXO-based transaction.
// It greedily selects from availableUTXOs until the total covers amount,
// builds a P2PKH output to toAddr, and sends change back to fromAddr if any.
// fromAddr and toAddr must be 40-char hex strings (output of AddressFromPub).
func CreateTransaction(
	priv ed25519.PrivateKey,
	fromAddr string,
	toAddr string,
	amount int64,
	availableUTXOs []ClientUTXO,
) (Transaction, error) {
	if amount <= 0 {
		return Transaction{}, errors.New("amount must be positive")
	}

	// Greedy input selection.
	var selected []ClientUTXO
	var total int64
	for _, u := range availableUTXOs {
		selected = append(selected, u)
		total += u.Out.Value
		if total >= amount {
			break
		}
	}
	if total < amount {
		return Transaction{}, fmt.Errorf("insufficient funds: have %d, need %d", total, amount)
	}

	// Build VINs with empty ScriptSig (filled by signing).
	vins := make([]VIN, len(selected))
	prevOuts := make([]VOUT, len(selected))
	for i, u := range selected {
		vins[i] = VIN{
			Txid:      u.Txid,
			Vout:      u.VoutIdx,
			ScriptSig: ScriptSig{},
		}
		prevOuts[i] = u.Out
	}

	// Build VOUTs: payment to toAddr + change to fromAddr.
	vouts := []VOUT{
		{
			Value:        amount,
			N:            0,
			ScriptPubKey: MakeP2PKHScriptPubKey(toAddr),
		},
	}
	if total > amount {
		vouts = append(vouts, VOUT{
			Value:        total - amount,
			N:            1,
			ScriptPubKey: MakeP2PKHScriptPubKey(fromAddr),
		})
	}

	tx := Transaction{
		Version:  1,
		Vin:      vins,
		Vout:     vouts,
		LockTime: 0,
	}

	if err := tx.SignEd25519(priv, prevOuts); err != nil {
		return Transaction{}, fmt.Errorf("signing failed: %w", err)
	}

	return tx, nil
}
