package core

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"coreservice/internal/wallet"
)

// Serialize returns Bitcoin-style wire encoding used for UTXO sighash / txid
// (same layout as orderingservice/types.Transaction.Serialize).
func (tx *Transaction) Serialize() []byte {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, tx.Version)

	writeVarInt(buf, uint64(len(tx.Vin)))
	for _, vin := range tx.Vin {
		prevBytes, _ := hexToBytesFixed32(vin.Txid)
		buf.Write(reverseBytes(prevBytes))
		_ = binary.Write(buf, binary.LittleEndian, uint32(vin.Vout))
		script, _ := hex.DecodeString(vin.ScriptSig.Hex)
		writeVarInt(buf, uint64(len(script)))
		buf.Write(script)
		_ = binary.Write(buf, binary.LittleEndian, uint32(0xffffffff))
	}

	writeVarInt(buf, uint64(len(tx.Vout)))
	for _, vout := range tx.Vout {
		_ = binary.Write(buf, binary.LittleEndian, uint64(vout.Value))
		scriptBytes, _ := hex.DecodeString(vout.ScriptPubKey.Hex)
		writeVarInt(buf, uint64(len(scriptBytes)))
		buf.Write(scriptBytes)
	}

	_ = binary.Write(buf, binary.LittleEndian, tx.LockTime)
	return buf.Bytes()
}

// ShallowCopyEmptySigs clears all ScriptSig fields (for sighash).
func (tx *Transaction) ShallowCopyEmptySigs() Transaction {
	newVin := make([]VIN, len(tx.Vin))
	for i := range tx.Vin {
		newVin[i] = VIN{Txid: tx.Vin[i].Txid, Vout: tx.Vin[i].Vout}
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

// SighashForInput returns the double-SHA256 digest signed for Vin[i]
// (prevOut ScriptPubKey.Hex injected into that input's ScriptSig).
func (tx *Transaction) SighashForInput(i int, prevOut VOUT) ([32]byte, error) {
	if i < 0 || i >= len(tx.Vin) {
		return [32]byte{}, fmt.Errorf("input index %d out of range", i)
	}
	txCopy := tx.ShallowCopyEmptySigs()
	txCopy.Vin[i].ScriptSig.Hex = prevOut.ScriptPubKey.Hex
	raw := txCopy.Serialize()
	h1 := sha256.Sum256(raw)
	return sha256.Sum256(h1[:]), nil
}

// VerifyVinScriptSigs checks each vin ScriptSig = sig(64) || pubkey(32) against prevOuts
// using Orderer-CLI Ed25519 sighash rules, and that pubkey matches locked address.
func (tx *Transaction) VerifyVinScriptSigs(prevOuts []VOUT) error {
	if len(prevOuts) != len(tx.Vin) {
		return fmt.Errorf("prevOuts length (%d) != vin length (%d)", len(prevOuts), len(tx.Vin))
	}
	for i, vin := range tx.Vin {
		script, err := hex.DecodeString(vin.ScriptSig.Hex)
		if err != nil {
			return fmt.Errorf("vin[%d]: bad scriptSig hex: %w", i, err)
		}
		if len(script) != ed25519.SignatureSize+ed25519.PublicKeySize {
			return fmt.Errorf("vin[%d]: scriptSig must be 96 bytes (sig||pubkey), got %d", i, len(script))
		}
		sig := script[:ed25519.SignatureSize]
		pub := ed25519.PublicKey(script[ed25519.SignatureSize:])

		addr := wallet.AddressFromPub(pub)
		if !prevOutLockedTo(prevOuts[i], addr) {
			return fmt.Errorf("vin[%d]: pubkey address %s does not unlock prevOut", i, addr)
		}

		digest, err := tx.SighashForInput(i, prevOuts[i])
		if err != nil {
			return err
		}
		if !ed25519.Verify(pub, digest[:], sig) {
			return fmt.Errorf("vin[%d]: invalid Ed25519 signature", i)
		}
	}
	return nil
}

// SignVinEd25519 signs each input (demo / Phase 6 helper). Recomputes nothing for SC txid.
func (tx *Transaction) SignVinEd25519(priv ed25519.PrivateKey, prevOuts []VOUT) error {
	if len(prevOuts) != len(tx.Vin) {
		return fmt.Errorf("prevOuts length (%d) must match Vin length (%d)", len(prevOuts), len(tx.Vin))
	}
	pub := priv.Public().(ed25519.PublicKey)
	for i := range tx.Vin {
		digest, err := tx.SighashForInput(i, prevOuts[i])
		if err != nil {
			return err
		}
		sig := ed25519.Sign(priv, digest[:])
		script := append(sig, pub...)
		tx.Vin[i].ScriptSig.Hex = hex.EncodeToString(script)
		tx.Vin[i].ScriptSig.ASM = fmt.Sprintf("%x %x", sig, pub)
	}
	return nil
}

func prevOutLockedTo(out VOUT, addr string) bool {
	for _, a := range out.ScriptPubKey.Addresses {
		if a == addr {
			return true
		}
	}
	want := "76a914" + addr + "88ac"
	return stringsEqualFoldHex(out.ScriptPubKey.Hex, want)
}

func stringsEqualFoldHex(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'F' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'F' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func writeVarInt(buf *bytes.Buffer, n uint64) {
	switch {
	case n < 0xfd:
		buf.WriteByte(byte(n))
	case n <= 0xffff:
		buf.WriteByte(0xfd)
		_ = binary.Write(buf, binary.LittleEndian, uint16(n))
	case n <= 0xffffffff:
		buf.WriteByte(0xfe)
		_ = binary.Write(buf, binary.LittleEndian, uint32(n))
	default:
		buf.WriteByte(0xff)
		_ = binary.Write(buf, binary.LittleEndian, n)
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
