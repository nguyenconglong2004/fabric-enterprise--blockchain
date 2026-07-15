package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/cloudflare/circl/sign/mldsa/mldsa44"
)

const mldsa44PrivateSize = mldsa44.PrivateKeySize
const mldsa44PubHexLen = mldsa44.PublicKeySize * 2
const mldsa44SigHexLen = mldsa44.SignatureSize * 2

type mldsa44Signer struct {
	privateKey    *mldsa44.PrivateKey
	privateKeyHex string
	publicKeyHex  string
}

func newMLDSA44Signer(privateKeyHex string) (*mldsa44Signer, error) {
	b, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid mldsa-44 private key hex: %w", err)
	}
	if len(b) != mldsa44.PrivateKeySize {
		return nil, fmt.Errorf("mldsa-44 private key must be %d bytes (%d hex chars), got %d",
			mldsa44.PrivateKeySize, mldsa44.PrivateKeySize*2, len(b))
	}
	var sk mldsa44.PrivateKey
	if err := sk.UnmarshalBinary(b); err != nil {
		return nil, fmt.Errorf("mldsa-44 unpack private key: %w", err)
	}
	pk := sk.Public().(*mldsa44.PublicKey)
	return &mldsa44Signer{
		privateKey:    &sk,
		privateKeyHex: privateKeyHex,
		publicKeyHex:  hex.EncodeToString(pk.Bytes()),
	}, nil
}

func generateMLDSA44Signer() (*mldsa44Signer, error) {
	_, sk, err := mldsa44.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("mldsa-44 generate: %w", err)
	}
	privHex := hex.EncodeToString(sk.Bytes())
	return &mldsa44Signer{
		privateKey:    sk,
		privateKeyHex: privHex,
		publicKeyHex:  hex.EncodeToString(sk.Public().(*mldsa44.PublicKey).Bytes()),
	}, nil
}

func (s *mldsa44Signer) Algorithm() Algorithm  { return AlgoMLDSA44 }
func (s *mldsa44Signer) PublicKeyHex() string  { return s.publicKeyHex }
func (s *mldsa44Signer) PrivateKeyHex() string { return s.privateKeyHex }
func (s *mldsa44Signer) TrustedKey() string    { return string(AlgoMLDSA44) + ":" + s.publicKeyHex }

func (s *mldsa44Signer) SignTx(txID, contractName string, payload []byte) (string, error) {
	msg := TxMessage(txID, contractName, payload)
	var sig [mldsa44.SignatureSize]byte
	if err := mldsa44.SignTo(s.privateKey, msg, nil, false, sig[:]); err != nil {
		return "", fmt.Errorf("mldsa-44 sign: %w", err)
	}
	return hex.EncodeToString(sig[:]), nil
}

func verifyMLDSA44Tx(txID, contractName string, payload []byte, sigHex, pubHex string) bool {
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != mldsa44.SignatureSize {
		return false
	}
	pubBytes, err := hex.DecodeString(pubHex)
	if err != nil || len(pubBytes) != mldsa44.PublicKeySize {
		return false
	}
	var pk mldsa44.PublicKey
	if err := pk.UnmarshalBinary(pubBytes); err != nil {
		return false
	}
	return mldsa44.Verify(&pk, TxMessage(txID, contractName, payload), nil, sig)
}

func (s *mldsa44Signer) VerifyTx(txID, contractName string, payload []byte, sigHex, pubHex string) bool {
	return verifyMLDSA44Tx(txID, contractName, payload, sigHex, pubHex)
}
