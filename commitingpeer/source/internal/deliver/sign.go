package deliver

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"

	"commiting-peer/internal/crypto"
	"commiting-peer/internal/types"
)

// TxSignProtocolID is the libp2p stream protocol for endorsing (signing) a
// transaction after simulation on core. Must match coreservice/network.CommitPeerTxSignProtocolID.
const TxSignProtocolID = "/fabric-enterprise/commit-peer/tx-sign/1.0.0"

// txSignResponse is the wire format for one round-trip on the tx-sign stream.
type txSignResponse struct {
	OK    bool               `json:"ok"`
	Error string             `json:"error,omitempty"`
	Tx    *types.Transaction `json:"tx,omitempty"`
}

// RegisterTxSignHandler registers a handler that signs incoming JSON transactions.
func (c *Client) RegisterTxSignHandler(signer crypto.Signer) {
	c.host.SetStreamHandler(protocol.ID(TxSignProtocolID), func(s network.Stream) {
		defer s.Close()

		remote := s.Conn().RemotePeer().ShortString()
		var tx types.Transaction
		if err := json.NewDecoder(s).Decode(&tx); err != nil {
			log.Printf("[tx-sign] decode from %s: %v", remote, err)
			_ = json.NewEncoder(s).Encode(txSignResponse{OK: false, Error: "invalid transaction JSON"})
			return
		}
		if tx.Txid == "" {
			_ = json.NewEncoder(s).Encode(txSignResponse{OK: false, Error: "missing txid"})
			return
		}

		if err := verifyExistingEndorsements(&tx); err != nil {
			log.Printf("[tx-sign] verify existing txid=%s: %v", tx.Txid, err)
			_ = json.NewEncoder(s).Encode(txSignResponse{OK: false, Error: err.Error()})
			return
		}

		sig, err := signer.SignTx(tx.Txid, tx.ContractName, tx.Payload)
		if err != nil {
			log.Printf("[tx-sign] sign txid=%s: %v", tx.Txid, err)
			_ = json.NewEncoder(s).Encode(txSignResponse{OK: false, Error: "sign failed"})
			return
		}
		pubHex := signer.PublicKeyHex()
		tx.Endorsements = appendOrReplaceEndorsement(tx.Endorsements, pubHex, sig, string(signer.Algorithm()))
		if len(tx.Endorsements) > 0 {
			last := tx.Endorsements[len(tx.Endorsements)-1]
			tx.SenderPubKey = last.PublicKey
			tx.Signature = last.Signature
		}
		if tx.ClientPubKey == "" {
			tx.ClientPubKey = pubHex
		}

		if !signer.VerifyTx(tx.Txid, tx.ContractName, tx.Payload, sig, pubHex) {
			log.Printf("[tx-sign] self-verify failed txid=%s", tx.Txid)
			_ = json.NewEncoder(s).Encode(txSignResponse{OK: false, Error: "signature self-verify failed"})
			return
		}

		if err := json.NewEncoder(s).Encode(txSignResponse{OK: true, Tx: &tx}); err != nil {
			log.Printf("[tx-sign] encode response txid=%s: %v", tx.Txid, err)
			return
		}
	})
}

func endorsementList(tx *types.Transaction) []types.EndorsementEntry {
	if len(tx.Endorsements) > 0 {
		return tx.Endorsements
	}
	return nil
}

func verifyExistingEndorsements(tx *types.Transaction) error {
	for i, e := range endorsementList(tx) {
		if e.PublicKey == "" || e.Signature == "" {
			return fmt.Errorf("endorsement %d: missing public_key or signature", i)
		}
		algo, err := crypto.ParseAlgorithm(e.Algorithm)
		if err != nil {
			return fmt.Errorf("endorsement %d: %w", i, err)
		}
		if !crypto.VerifyEndorsement(tx.Txid, tx.ContractName, tx.Payload, algo, e.Signature, e.PublicKey) {
			return fmt.Errorf("endorsement %d: invalid signature", i)
		}
	}
	return nil
}

func appendOrReplaceEndorsement(entries []types.EndorsementEntry, pubHex, sigHex, algo string) []types.EndorsementEntry {
	pubHex = strings.TrimSpace(pubHex)
	out := make([]types.EndorsementEntry, 0, len(entries)+1)
	replaced := false
	for _, e := range entries {
		if strings.EqualFold(strings.TrimSpace(e.PublicKey), pubHex) {
			out = append(out, types.EndorsementEntry{
				PublicKey: strings.TrimSpace(e.PublicKey),
				Signature: sigHex,
				Algorithm: algo,
			})
			replaced = true
		} else {
			out = append(out, e)
		}
	}
	if !replaced {
		out = append(out, types.EndorsementEntry{
			PublicKey: pubHex,
			Signature: sigHex,
			Algorithm: algo,
		})
	}
	return out
}
