package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

const ed25519PrivateSize = ed25519.PrivateKeySize

type ed25519Signer struct {
	privateKey ed25519.PrivateKey
	privateKeyHex string
	publicKeyHex  string
}

func newEd25519Signer(privateKeyHex string) (*ed25519Signer, error) {
	b, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid ed25519 private key hex: %w", err)
	}
	if len(b) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("ed25519 private key must be %d bytes (%d hex chars), got %d",
			ed25519.PrivateKeySize, ed25519.PrivateKeySize*2, len(b))
	}
	priv := ed25519.PrivateKey(b)
	return &ed25519Signer{
		privateKey:    priv,
		privateKeyHex: privateKeyHex,
		publicKeyHex:  hex.EncodeToString(priv.Public().(ed25519.PublicKey)),
	}, nil
}

func generateEd25519Signer() (*ed25519Signer, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("ed25519 generate: %w", err)
	}
	privHex := hex.EncodeToString(priv)
	return &ed25519Signer{
		privateKey:    priv,
		privateKeyHex: privHex,
		publicKeyHex:  hex.EncodeToString(pub),
	}, nil
}

func (s *ed25519Signer) Algorithm() Algorithm  { return AlgoEd25519 }
func (s *ed25519Signer) PublicKeyHex() string  { return s.publicKeyHex }
func (s *ed25519Signer) PrivateKeyHex() string { return s.privateKeyHex }
func (s *ed25519Signer) TrustedKey() string    { return string(AlgoEd25519) + ":" + s.publicKeyHex }

func (s *ed25519Signer) SignTx(txID, contractName string, payload []byte) (string, error) {
	msg := TxMessage(txID, contractName, payload)
	sig := ed25519.Sign(s.privateKey, msg)
	return hex.EncodeToString(sig), nil
}

func verifyEd25519Tx(txID, contractName string, payload []byte, sigHex, pubHex string) bool {
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	pub, err := hex.DecodeString(pubHex)
	if err != nil {
		return false
	}
	if len(pub) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pub), TxMessage(txID, contractName, payload), sig)
}

func (s *ed25519Signer) VerifyTx(txID, contractName string, payload []byte, sigHex, pubHex string) bool {
	return verifyEd25519Tx(txID, contractName, payload, sigHex, pubHex)
}
