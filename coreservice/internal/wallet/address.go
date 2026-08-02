package wallet

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"golang.org/x/crypto/ripemd160" //nolint:staticcheck
)

// HashPubKey: SHA256 then RIPEMD160 (same as orderingservice CLI).
func HashPubKey(pubkey []byte) []byte {
	sha := sha256.Sum256(pubkey)
	rip := ripemd160.New()
	rip.Write(sha[:])
	return rip.Sum(nil)
}

// AddressFromPub returns 40-char hex P2PKH address (Orderer CLI style).
func AddressFromPub(pub ed25519.PublicKey) string {
	return hex.EncodeToString(HashPubKey(pub))
}

// MakeP2PKHScriptPubKey builds locking script 76a914{addr}88ac.
func MakeP2PKHScriptPubKey(addr string) (asm, scriptHex string, addresses []string) {
	return "OP_DUP OP_HASH160 " + addr + " OP_EQUALVERIFY OP_CHECKSIG",
		"76a914" + addr + "88ac",
		[]string{addr}
}

// NewKeypair generates Ed25519 seed/priv/pub.
func NewKeypair() (seed []byte, priv ed25519.PrivateKey, pub ed25519.PublicKey, err error) {
	seed = make([]byte, ed25519.SeedSize)
	if _, err = rand.Read(seed); err != nil {
		return nil, nil, nil, err
	}
	priv = ed25519.NewKeyFromSeed(seed)
	pub = priv.Public().(ed25519.PublicKey)
	return seed, priv, pub, nil
}

// PrivFromSeedHex recovers private key from hex seed.
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
