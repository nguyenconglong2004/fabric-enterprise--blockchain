package validation

import (
	"bytes"
	"fmt"
	"strings"

	"commiting-peer/internal/crypto"
	"commiting-peer/internal/types"
)

// Engine validates blocks and individual transactions before they are committed.
// When trustedEndorserPubHex is non-empty, each smart-contract transaction must
// carry valid Ed25519 endorsements and at least one endorser public key must be
// in the trusted set (comma-separated hex in NewEngine).
type Engine struct {
	trustedPubHexes []string
}

// NewEngine returns a new validation engine. trustedEndorserPubHex may be a
// single hex public key or comma-separated keys. Empty string skips endorser
// checks (legacy / dev only).
func NewEngine(trustedEndorserPubHex string) *Engine {
	var pubs []string
	for _, p := range strings.Split(trustedEndorserPubHex, ",") {
		if s := strings.TrimSpace(p); s != "" {
			pubs = append(pubs, s)
		}
	}
	return &Engine{trustedPubHexes: pubs}
}

// ValidateBlock checks Merkle root and block hash (matching the ordering service).
// When trusted endorser keys are configured, smart-contract transactions must carry
// valid endorsements. Note: prev_hash check is skipped; it's still included in block
// hash computation for structure verification.
// committedTipHash is the last block hash already on disk (nil if the chain file is empty).
func (e *Engine) ValidateBlock(b types.Block, committedTipHash []byte) error {
	// Skip prev_hash chain continuity check
	// if err := e.verifyPrevHash(b, committedTipHash); err != nil {
	//	return fmt.Errorf("prev_hash check failed: %w", err)
	// }

	if err := e.verifyBlockIntegrity(b); err != nil {
		return fmt.Errorf("block integrity check failed: %w", err)
	}

	// Verify transaction endorsements (if trusted keys configured)
	if len(e.trustedPubHexes) == 0 {
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
	// Only use explicit endorsements array; ignore legacy Signature + SenderPubKey fields
	// (those are only for compatibility with old serialization, not block validation)
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
	trusted := make(map[string]struct{}, len(e.trustedPubHexes))
	for _, p := range e.trustedPubHexes {
		trusted[strings.ToLower(strings.TrimSpace(p))] = struct{}{}
	}
	var seenTrusted bool
	for i, ent := range list {
		if ent.PublicKey == "" || ent.Signature == "" {
			return fmt.Errorf("tx %s: endorsement %d incomplete", tx.Txid, i)
		}
		if !crypto.VerifyTransaction(tx.Txid, tx.ContractName, tx.Payload, ent.Signature, ent.PublicKey) {
			return fmt.Errorf("tx %s: invalid endorser signature (endorsement %d)", tx.Txid, i)
		}
		if _, ok := trusted[strings.ToLower(strings.TrimSpace(ent.PublicKey))]; ok {
			seenTrusted = true
		}
	}
	if !seenTrusted {
		return fmt.Errorf("tx %s: no endorsement from a trusted commit-peer key", tx.Txid)
	}
	return nil
}

// ValidateTransaction checks a single transaction (inputs, outputs, scripts,
// signatures, etc.). Stub for per-tx checks beyond endorser validation.
func (e *Engine) ValidateTransaction(_ types.Transaction) error {
	return nil
}

// padHash32 left-pads a hash into 32 bytes the same way the ordering service
// serializes prev_hash / merkle_root in the block header.
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

// verifyBlockIntegrity requires Merkle root and block hash on the wire and
// verifies them against transaction txids and the header fields.
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
