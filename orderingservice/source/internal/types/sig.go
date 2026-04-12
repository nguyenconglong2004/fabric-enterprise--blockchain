package types

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"golang.org/x/crypto/ripemd160" //nolint:staticcheck
)

// HashPubKey hashes a public key using SHA256 then RIPEMD160 (Bitcoin-style address derivation).
// Returns 20 bytes.
func HashPubKey(pubkey []byte) []byte {
	sha := sha256.Sum256(pubkey)
	rip := ripemd160.New()
	rip.Write(sha[:])
	return rip.Sum(nil)
}

// NewEd25519Keypair generates a new Ed25519 keypair from a random seed.
// Returns seed (32 bytes), private key (64 bytes), public key (32 bytes).
func NewEd25519Keypair() (seed []byte, priv ed25519.PrivateKey, pub ed25519.PublicKey, err error) {
	seed = make([]byte, ed25519.SeedSize)
	if _, err = rand.Read(seed); err != nil {
		return nil, nil, nil, err
	}
	priv = ed25519.NewKeyFromSeed(seed)
	pub = priv.Public().(ed25519.PublicKey)
	return seed, priv, pub, nil
}

// AddressFromPub derives a P2PKH address from an Ed25519 public key.
// Address = hex(SHA256(pubkey) → RIPEMD160), 20 bytes = 40 hex chars.
func AddressFromPub(pub ed25519.PublicKey) string {
	return hex.EncodeToString(HashPubKey(pub))
}

// MakeP2PKHScriptPubKey creates a standard Pay-to-Public-Key-Hash locking script.
// addr must be the 40-char hex of the 20-byte pubKeyHash (from AddressFromPub).
// Output script hex: 76a914{addr}88ac
func MakeP2PKHScriptPubKey(addr string) ScriptPubKey {
	return ScriptPubKey{
		ASM:       "OP_DUP OP_HASH160 " + addr + " OP_EQUALVERIFY OP_CHECKSIG",
		Hex:       "76a914" + addr + "88ac",
		Addresses: []string{addr},
	}
}

// SeedToHex encodes an Ed25519 seed to a hex string.
func SeedToHex(seed []byte) string {
	return hex.EncodeToString(seed)
}

// PrivFromSeedHex recovers an Ed25519 private key from a hex-encoded seed.
func PrivFromSeedHex(seedHex string) (ed25519.PrivateKey, error) {
	b, err := hex.DecodeString(seedHex)
	if err != nil {
		return nil, fmt.Errorf("invalid seed hex: %w", err)
	}
	if len(b) != ed25519.SeedSize {
		return nil, errors.New("invalid seed length: expected 32 bytes")
	}
	return ed25519.NewKeyFromSeed(b), nil
}
