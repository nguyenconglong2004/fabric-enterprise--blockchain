package validation

import (
	"bytes"
	"fmt"
	"strings"

	"commiting-peer/internal/crypto"
	"commiting-peer/internal/types"
)

type trustedKey struct {
	algo   crypto.Algorithm
	pubHex string
}

// Engine validates blocks and individual transactions before they are committed.
type Engine struct {
	trusted []trustedKey
}

// NewEngine returns a new validation engine. trustedEndorserPubHex may be a
// single hex public key, comma-separated keys, or "algo:hex" entries.
// Empty string skips endorser checks (legacy / dev only).
func NewEngine(trustedEndorserPubHex string) *Engine {
	var keys []trustedKey
	for _, p := range strings.Split(trustedEndorserPubHex, ",") {
		algo, pub, err := crypto.ParseTrustedKey(p)
		if err != nil || pub == "" {
			continue
		}
		keys = append(keys, trustedKey{algo: algo, pubHex: strings.ToLower(pub)})
	}
	return &Engine{trusted: keys}
}

// ValidateBlock checks Merkle root and block hash (matching the ordering service).
func (e *Engine) ValidateBlock(b types.Block, committedTipHash []byte) error {
	if err := e.verifyBlockIntegrity(b); err != nil {
		return fmt.Errorf("block integrity check failed: %w", err)
	}

	if len(e.trusted) == 0 {
		return nil
	}
	for _, tx := range b.Transactions {
		if err := e.validateEndorsedTx(tx); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) endorsementList(tx types.Transaction) []types.EndorsementEntry {
	if len(tx.Endorsements) > 0 {
		return tx.Endorsements
	}
	return nil
}

func (e *Engine) validateEndorsedTx(tx types.Transaction) error {
	if len(tx.Payload) == 0 || tx.ContractName == "" {
		return nil
	}
	list := e.endorsementList(tx)
	if len(list) == 0 {
		return fmt.Errorf("tx %s: missing endorsements", tx.Txid)
	}

	trusted := make(map[string]struct{}, len(e.trusted))
	for _, tk := range e.trusted {
		trusted[trustedKeyID(tk.algo, tk.pubHex)] = struct{}{}
	}

	var seenTrusted bool
	for i, ent := range list {
		if ent.PublicKey == "" || ent.Signature == "" {
			return fmt.Errorf("tx %s: endorsement %d incomplete", tx.Txid, i)
		}
		algo, err := crypto.ParseAlgorithm(ent.Algorithm)
		if err != nil {
			return fmt.Errorf("tx %s: endorsement %d: %w", tx.Txid, i, err)
		}
		if algo == "" {
			algo = crypto.InferAlgorithmFromWire(ent.Signature, ent.PublicKey)
		}
		if !crypto.VerifyEndorsement(tx.Txid, tx.ContractName, tx.Payload, algo, ent.Signature, ent.PublicKey) {
			return fmt.Errorf("tx %s: invalid endorser signature (endorsement %d)", tx.Txid, i)
		}
		if _, ok := trusted[trustedKeyID(algo, strings.ToLower(strings.TrimSpace(ent.PublicKey)))]; ok {
			seenTrusted = true
		}
	}
	if !seenTrusted {
		return fmt.Errorf("tx %s: no endorsement from a trusted commit-peer key", tx.Txid)
	}
	return nil
}

func trustedKeyID(algo crypto.Algorithm, pubHex string) string {
	return string(algo) + ":" + strings.ToLower(strings.TrimSpace(pubHex))
}

// ValidateTransaction checks a single transaction (stub).
func (e *Engine) ValidateTransaction(_ types.Transaction) error {
	return nil
}

func padHash32(h []byte) [32]byte {
	var out [32]byte
	if len(h) == 0 {
		return out
	}
	if len(h) <= 32 {
		copy(out[32-len(h):], h)
		return out
	}
	copy(out[:], h[len(h)-32:])
	return out
}

func (e *Engine) verifyPrevHash(b types.Block, committedTip []byte) error {
	want := padHash32(committedTip)
	got := padHash32(b.PrevHash)
	if !bytes.Equal(want[:], got[:]) {
		return fmt.Errorf("expected prev_hash %x (local tip), got %x", want[:], got[:])
	}
	return nil
}

func (e *Engine) verifyBlockIntegrity(b types.Block) error {
	if len(b.MerkleRoot) == 0 || len(b.Hash) == 0 {
		return fmt.Errorf("block must include non-empty merkle_root and hash")
	}

	txids := make([]string, len(b.Transactions))
	for i, tx := range b.Transactions {
		txids[i] = tx.Txid
	}

	merkleRootHex := fmt.Sprintf("%x", b.MerkleRoot)
	if err := crypto.VerifyBlockMerkleRoot(txids, merkleRootHex); err != nil {
		return fmt.Errorf("merkle root verification failed: %w", err)
	}

	hashHex := fmt.Sprintf("%x", b.Hash)
	prevHashHex := fmt.Sprintf("%x", b.PrevHash)
	if err := crypto.VerifyBlockHash(b.Timestamp, b.Nonce, prevHashHex, merkleRootHex, hashHex); err != nil {
		return fmt.Errorf("block hash verification failed: %w", err)
	}

	return nil
}
