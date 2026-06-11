package loadgen

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"raft-order-service/internal/types"
)

// DefaultClientPubKey is a valid 32-byte Ed25519 public key (hex) for orderer Validate().
const DefaultClientPubKey = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"

// TxOptions configures synthetic smart-contract transactions.
type TxOptions struct {
	Prefix       string
	ContractName string
	FunctionName string
	ClientPubKey string
}

// NewSmartContractTx builds a minimal smart-contract transaction accepted by the orderer.
func NewSmartContractTx(seq int64, opts TxOptions) (types.Transaction, error) {
	if opts.Prefix == "" {
		opts.Prefix = "loadgen-"
	}
	if opts.ContractName == "" {
		opts.ContractName = "bench_ping"
	}
	if opts.FunctionName == "" {
		opts.FunctionName = "execute"
	}
	if opts.ClientPubKey == "" {
		opts.ClientPubKey = DefaultClientPubKey
	}

	payloadObj := map[string]string{
		"v": fmt.Sprintf("%s%d-%d", opts.Prefix, seq, time.Now().UnixNano()),
	}
	payloadJSON, err := json.Marshal(payloadObj)
	if err != nil {
		return types.Transaction{}, err
	}

	tx := types.Transaction{
		Version:      1,
		LockTime:     0,
		Txid:         fmt.Sprintf("%s%d-%d", opts.Prefix, seq, time.Now().UnixNano()),
		ClientPubKey: opts.ClientPubKey,
		ContractName: opts.ContractName,
		FunctionName: opts.FunctionName,
		Payload:      payloadJSON,
		Vin:          []types.VIN{},
		Vout:         []types.VOUT{},
	}

	if err := tx.Validate(); err != nil {
		return types.Transaction{}, err
	}
	return tx, nil
}

// PayloadHex returns the wire-format hex payload (for logging).
func PayloadHex(payload []byte) string {
	return hex.EncodeToString(payload)
}
