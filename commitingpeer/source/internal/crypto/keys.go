package crypto

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

// ============================================
// BLOCK VERIFICATION (Hash + Merkle Root)
// ============================================

// HashBlock computes the double-SHA256 of the block header, matching orderingservice/internal/types.Block.SerializeHeader.
func HashBlock(timestamp int64, nonce int, prevHashHex string, merkleRootHex string) string {
	buf := new(bytes.Buffer)

	binary.Write(buf, binary.LittleEndian, uint32(1))

	prev := make([]byte, 32)
	if prevHashHex != "" {
		prevBytes, _ := hex.DecodeString(prevHashHex)
		copy(prev[32-len(prevBytes):], prevBytes)
	}
	buf.Write(prev)

	mr := make([]byte, 32)
	if merkleRootHex != "" {
		mrBytes, _ := hex.DecodeString(merkleRootHex)
		copy(mr[32-len(mrBytes):], mrBytes)
	}
	buf.Write(mr)

	binary.Write(buf, binary.LittleEndian, uint32(timestamp))
	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint32(nonce))

	h1 := sha256.Sum256(buf.Bytes())
	h2 := sha256.Sum256(h1[:])
	return hex.EncodeToString(h2[:])
}

// ComputeMerkleRoot matches orderingservice/internal/types.ComputeMerkleRoot.
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

// VerifyBlockMerkleRoot verifies the Merkle root from transaction ids.
func VerifyBlockMerkleRoot(txids []string, storedMerkleRootHex string) error {
	computed := ComputeMerkleRoot(txids)
	if computed != storedMerkleRootHex {
		return fmt.Errorf("merkle root mismatch: computed %s, stored %s", computed, storedMerkleRootHex)
	}
	return nil
}
