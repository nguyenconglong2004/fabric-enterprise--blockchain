package crypto

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// Algorithm names the endorsement signing scheme.
type Algorithm string

const (
	AlgoEd25519 Algorithm = "ed25519"
	AlgoMLDSA44 Algorithm = "mldsa-44"
)

// Signer signs and verifies transaction endorsements.
type Signer interface {
	Algorithm() Algorithm
	PublicKeyHex() string
	SignTx(txID, contractName string, payload []byte) (sigHex string, err error)
	VerifyTx(txID, contractName string, payload []byte, sigHex, pubHex string) bool
	// TrustedKey returns "algo:pubhex" for TRUSTED_ENDORSER_PUBLIC_KEYS.
	TrustedKey() string
	PrivateKeyHex() string
}

// TxMessage is the signed payload (same layout as core / orderer).
func TxMessage(txID, contractName string, payload []byte) []byte {
	return []byte(txID + contractName + string(payload))
}

// ParseAlgorithm normalizes COMMIT_PEER_KEY_ALGO (default ed25519).
func ParseAlgorithm(raw string) (Algorithm, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "", "1", "ed25519":
		return AlgoEd25519, nil
	case "2", "mldsa", "mldsa44", "mldsa-44", "ml-dsa-44":
		return AlgoMLDSA44, nil
	default:
		return "", fmt.Errorf("unsupported key algorithm %q (use ed25519 or mldsa-44)", raw)
	}
}

// ResolveKeyAlgorithm infers algorithm from private key hex length.
func ResolveKeyAlgorithm(privHex string) (Algorithm, error) {
	b, err := hex.DecodeString(strings.TrimSpace(privHex))
	if err != nil {
		return "", fmt.Errorf("invalid private key hex: %w", err)
	}
	switch len(b) {
	case ed25519PrivateSize:
		return AlgoEd25519, nil
	case mldsa44PrivateSize:
		return AlgoMLDSA44, nil
	default:
		return "", fmt.Errorf("unknown private key size %d bytes (ed25519=%d, mldsa-44=%d)",
			len(b), ed25519PrivateSize, mldsa44PrivateSize)
	}
}

// NewSigner builds a signer for algo from an existing private key hex.
func NewSigner(algo Algorithm, privateKeyHex string) (Signer, error) {
	switch algo {
	case AlgoEd25519, "":
		return newEd25519Signer(privateKeyHex)
	case AlgoMLDSA44:
		return newMLDSA44Signer(privateKeyHex)
	default:
		return nil, fmt.Errorf("unsupported algorithm %q", algo)
	}
}

// GenerateSigner creates a new key pair for algo.
func GenerateSigner(algo Algorithm) (Signer, error) {
	switch algo {
	case AlgoEd25519, "":
		return generateEd25519Signer()
	case AlgoMLDSA44:
		return generateMLDSA44Signer()
	default:
		return nil, fmt.Errorf("unsupported algorithm %q", algo)
	}
}

func defaultKeyFile(algo Algorithm) string {
	if path := strings.TrimSpace(os.Getenv("COMMIT_PEER_KEY_FILE")); path != "" {
		return path
	}
	switch algo {
	case AlgoMLDSA44:
		return "endorsement.mldsa44.key"
	default:
		return "endorsement.key"
	}
}

// LoadOrGenerateSigner loads private key from env/file or generates a new one.
func LoadOrGenerateSigner(algo Algorithm) (Signer, error) {
	if envAlgo := strings.TrimSpace(os.Getenv("COMMIT_PEER_KEY_ALGO")); envAlgo != "" && algo == "" {
		var err error
		algo, err = ParseAlgorithm(envAlgo)
		if err != nil {
			return nil, err
		}
	}
	if algo == "" {
		algo = AlgoEd25519
	}

	priv := strings.TrimSpace(os.Getenv("COMMIT_PEER_PRIVATE_KEY"))
	keyPath := defaultKeyFile(algo)
	if priv == "" {
		if b, err := os.ReadFile(keyPath); err == nil {
			priv = strings.TrimSpace(string(b))
		}
	}

	if priv != "" {
		resolved, err := ResolveKeyAlgorithm(priv)
		if err != nil {
			return nil, err
		}
		if resolved != algo {
			return nil, fmt.Errorf("key file is %s but COMMIT_PEER_KEY_ALGO=%s", resolved, algo)
		}
		return NewSigner(algo, priv)
	}

	s, err := GenerateSigner(algo)
	if err != nil {
		return nil, err
	}
	if saveErr := os.WriteFile(keyPath, []byte(s.PrivateKeyHex()), 0600); saveErr != nil {
		return s, fmt.Errorf("generated key but could not save to %s: %w", keyPath, saveErr)
	}
	return s, nil
}

// InferAlgorithmFromWire guesses the scheme when algorithm is missing (e.g. orderer
// relay before algorithm field was added). ML-DSA-44 keys/signatures are much larger
// than Ed25519 and do not overlap in hex length.
func InferAlgorithmFromWire(sigHex, pubHex string) Algorithm {
	sigHex = strings.TrimSpace(sigHex)
	pubHex = strings.TrimSpace(pubHex)
	if len(pubHex) == mldsa44PubHexLen && len(sigHex) == mldsa44SigHexLen {
		return AlgoMLDSA44
	}
	return AlgoEd25519
}

// VerifyEndorsement checks one endorsement entry (empty algorithm = infer from wire).
func VerifyEndorsement(txID, contractName string, payload []byte, algo Algorithm, sigHex, pubHex string) bool {
	if algo == "" {
		algo = InferAlgorithmFromWire(sigHex, pubHex)
	}
	switch algo {
	case AlgoEd25519:
		return verifyEd25519Tx(txID, contractName, payload, sigHex, pubHex)
	case AlgoMLDSA44:
		return verifyMLDSA44Tx(txID, contractName, payload, sigHex, pubHex)
	default:
		return false
	}
}

// ParseTrustedKey parses "algo:hex" or bare hex (ed25519).
func ParseTrustedKey(raw string) (Algorithm, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("empty trusted key")
	}
	if i := strings.Index(raw, ":"); i > 0 {
		algo, err := ParseAlgorithm(raw[:i])
		if err != nil {
			return "", "", err
		}
		return algo, strings.TrimSpace(raw[i+1:]), nil
	}
	return AlgoEd25519, raw, nil
}

// SignTxMessage signs with Ed25519 (legacy helper).
func SignTxMessage(txID, contractName string, payload []byte, privateKeyHex string) (string, error) {
	s, err := newEd25519Signer(privateKeyHex)
	if err != nil {
		return "", err
	}
	return s.SignTx(txID, contractName, payload)
}

// VerifyTxMessage verifies with Ed25519 (legacy helper).
func VerifyTxMessage(txID, contractName string, payload []byte, signatureHex, publicKeyHex string) bool {
	return verifyEd25519Tx(txID, contractName, payload, signatureHex, publicKeyHex)
}

// GenerateKeyPair generates Ed25519 keys (legacy helper).
func GenerateKeyPair() (*KeyPair, error) {
	s, err := generateEd25519Signer()
	if err != nil {
		return nil, err
	}
	return &KeyPair{
		PublicKey:  s.PublicKeyHex(),
		PrivateKey: s.PrivateKeyHex(),
	}, nil
}

// PublicKeyFromPrivateHex derives Ed25519 public key hex.
func PublicKeyFromPrivateHex(privateKeyHex string) (string, error) {
	s, err := newEd25519Signer(privateKeyHex)
	if err != nil {
		return "", err
	}
	return s.PublicKeyHex(), nil
}

// KeyPair holds hex-encoded keys (legacy).
type KeyPair struct {
	PublicKey  string
	PrivateKey string
}

// SignTransaction is a legacy alias for SignTxMessage.
func SignTransaction(txID, contractName string, payload []byte, privateKeyHex string) (string, error) {
	return SignTxMessage(txID, contractName, payload, privateKeyHex)
}

// VerifyTransaction is a legacy alias for VerifyTxMessage.
func VerifyTransaction(txID, contractName string, payload []byte, signatureHex, publicKeyHex string) bool {
	return VerifyTxMessage(txID, contractName, payload, signatureHex, publicKeyHex)
}
