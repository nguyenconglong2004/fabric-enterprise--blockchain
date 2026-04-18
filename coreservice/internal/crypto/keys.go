package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// KeyPair holds public and private keys
type KeyPair struct {
	PublicKey  string
	PrivateKey string
}

// GenerateKeyPair generates a new ED25519 key pair
// Returns (publicKey, privateKey) both as hex strings
func GenerateKeyPair() (*KeyPair, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %w", err)
	}

	return &KeyPair{
		PublicKey:  hex.EncodeToString(publicKey),
		PrivateKey: hex.EncodeToString(privateKey),
	}, nil
}

// Sign signs a message with the private key
// Returns signature as hex string
func Sign(message []byte, privateKeyHex string) (string, error) {
	privateKeyBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return "", fmt.Errorf("invalid private key format: %w", err)
	}

	privateKey := ed25519.PrivateKey(privateKeyBytes)
	signature := ed25519.Sign(privateKey, message)

	return hex.EncodeToString(signature), nil
}

// Verify verifies a signature with the public key
// Returns true if signature is valid, false otherwise
func Verify(message []byte, signatureHex string, publicKeyHex string) bool {
	signatureBytes, err := hex.DecodeString(signatureHex)
	if err != nil {
		return false
	}

	publicKeyBytes, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		return false
	}

	publicKey := ed25519.PublicKey(publicKeyBytes)
	return ed25519.Verify(publicKey, message, signatureBytes)
}

// SignTransaction signs a transaction by combining all relevant fields
func SignTransaction(txID string, contractName string, payload []byte, privateKeyHex string) (string, error) {
	// Create message from transaction fields
	message := txID + contractName + string(payload)
	return Sign([]byte(message), privateKeyHex)
}

// VerifyTransaction verifies a transaction signature
func VerifyTransaction(txID string, contractName string, payload []byte, signatureHex string, publicKeyHex string) bool {
	message := txID + contractName + string(payload)
	return Verify([]byte(message), signatureHex, publicKeyHex)
}
