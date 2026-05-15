package crypto

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

// KeyPair holds public and private keys as hex strings (same wire format as core service).
type KeyPair struct {
	PublicKey  string
	PrivateKey string
}

// GenerateKeyPair generates a new ED25519 key pair (for one-off setup / tooling only).
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

// PublicKeyFromPrivateHex derives the public key hex from a full Ed25519 private key hex.
func PublicKeyFromPrivateHex(privateKeyHex string) (string, error) {
	privateKeyBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return "", fmt.Errorf("invalid private key hex: %w", err)
	}
	if len(privateKeyBytes) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("private key must be %d bytes (%d hex chars), got %d",
			ed25519.PrivateKeySize, ed25519.PrivateKeySize*2, len(privateKeyBytes))
	}
	priv := ed25519.PrivateKey(privateKeyBytes)
	return hex.EncodeToString(priv.Public().(ed25519.PublicKey)), nil
}

func Sign(message []byte, privateKeyHex string) (string, error) {
	privateKeyBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return "", fmt.Errorf("invalid private key format: %w", err)
	}
	privateKey := ed25519.PrivateKey(privateKeyBytes)
	signature := ed25519.Sign(privateKey, message)
	return hex.EncodeToString(signature), nil
}

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

// SignTransaction signs using the same message layout as core service / orderer.
func SignTransaction(txID string, contractName string, payload []byte, privateKeyHex string) (string, error) {
	message := txID + contractName + string(payload)
	return Sign([]byte(message), privateKeyHex)
}

func VerifyTransaction(txID string, contractName string, payload []byte, signatureHex string, publicKeyHex string) bool {
	message := txID + contractName + string(payload)
	return Verify([]byte(message), signatureHex, publicKeyHex)
}

// ============================================
// BLOCK VERIFICATION (Hash + Merkle Root)
// ============================================

// HashBlock computes the double-SHA256 of the block header, matching orderingservice/internal/types.Block.SerializeHeader.
// Layout: version (LE u32) + prevHash (32) + merkleRoot (32) + timestamp (LE u32) + bits (LE u32) + nonce (LE u32).
func HashBlock(timestamp int64, nonce int, prevHashHex string, merkleRootHex string) string {
	buf := new(bytes.Buffer)

	// version = 1
	binary.Write(buf, binary.LittleEndian, uint32(1))

	// prevHash (pad to 32 bytes)
	prev := make([]byte, 32)
	if prevHashHex != "" {
		prevBytes, _ := hex.DecodeString(prevHashHex)
		copy(prev[32-len(prevBytes):], prevBytes)
	}
	buf.Write(prev)

	// merkleRoot (pad to 32 bytes)
	mr := make([]byte, 32)
	if merkleRootHex != "" {
		mrBytes, _ := hex.DecodeString(merkleRootHex)
		copy(mr[32-len(mrBytes):], mrBytes)
	}
	buf.Write(mr)

	// timestamp (4 bytes, LE)
	binary.Write(buf, binary.LittleEndian, uint32(timestamp))

	// bits = 0 (difficulty, not used)
	binary.Write(buf, binary.LittleEndian, uint32(0))

	binary.Write(buf, binary.LittleEndian, uint32(nonce))

	// Double SHA256
	h1 := sha256.Sum256(buf.Bytes())
	h2 := sha256.Sum256(h1[:])
	return hex.EncodeToString(h2[:])
}

// ComputeMerkleRoot matches orderingservice/internal/types.ComputeMerkleRoot: txids are the
// Merkle leaves (as UTF-8/byte strings). For 0 txs: all-zero root; for 1 tx: double-SHA256(txid);
// for 2+: first level concatenates up to 32 bytes from each child slice into a 64-byte buffer
// (copy semantics), then double-SHA256; upper levels use fixed 32-byte children.
func ComputeMerkleRoot(txids []string) string {
	n := len(txids)
	if n == 0 {
		return hex.EncodeToString(make([]byte, 32))
	}
	if n == 1 {
		h1 := sha256.Sum256([]byte(txids[0]))
		h2 := sha256.Sum256(h1[:])
		return hex.EncodeToString(h2[:])
	}

	hashes := make([][]byte, n)
	for i := range txids {
		hashes[i] = []byte(txids[i])
	}

	buf := make([]byte, 64)
	for len(hashes) > 1 {
		nextLen := (len(hashes) + 1) / 2
		nextLevel := make([][]byte, 0, nextLen)
		for i := 0; i < len(hashes); i += 2 {
			copy(buf[:32], hashes[i])
			if i+1 < len(hashes) {
				copy(buf[32:], hashes[i+1])
			} else {
				copy(buf[32:], hashes[i])
			}
			h1 := sha256.Sum256(buf)
			h2 := sha256.Sum256(h1[:])
			hash := make([]byte, 32)
			copy(hash, h2[:])
			nextLevel = append(nextLevel, hash)
		}
		hashes = nextLevel
	}

	return hex.EncodeToString(hashes[0])
}

// VerifyBlockHash verifies that the block's stored hash matches the computed hash.
func VerifyBlockHash(blockTimestamp int64, blockNonce int, prevHashHex string, merkleRootHex string, storedHashHex string) error {
	computed := HashBlock(blockTimestamp, blockNonce, prevHashHex, merkleRootHex)
	if computed != storedHashHex {
		return fmt.Errorf("block hash mismatch: computed %s, stored %s", computed, storedHashHex)
	}
	return nil
}

// VerifyBlockMerkleRoot verifies the Merkle root from the list of transaction ids (wire txid strings).
func VerifyBlockMerkleRoot(txids []string, storedMerkleRootHex string) error {
	computed := ComputeMerkleRoot(txids)
	if computed != storedMerkleRootHex {
		return fmt.Errorf("merkle root mismatch: computed %s, stored %s", computed, storedMerkleRootHex)
	}
	return nil
}
