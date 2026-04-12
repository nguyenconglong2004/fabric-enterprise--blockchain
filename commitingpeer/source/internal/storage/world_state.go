package storage

import (
	"encoding/json"
	"fmt"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"

	"commiting-peer/internal/types"
)

// UTXOEntry is a single entry returned by AllUTXOs.
type UTXOEntry struct {
	Txid  string
	Index int
	Out   types.VOUT
}

// WorldState is an UTXO-based world state backed by LevelDB.
//
// Key schema:
//
//	utxo:<txid>:<vout_index>  →  JSON-encoded types.VOUT
//
// When a block is applied:
//   - Each VIN removes the UTXO it spends.
//   - Each VOUT creates a new UTXO entry.
type WorldState struct {
	db *leveldb.DB
}

// NewWorldState opens (or creates) a LevelDB database at path.
func NewWorldState(path string) (*WorldState, error) {
	db, err := leveldb.OpenFile(path, nil)
	if err != nil {
		return nil, fmt.Errorf("world state: open leveldb at %q: %w", path, err)
	}
	return &WorldState{db: db}, nil
}

// ApplyBlock atomically updates the UTXO set for every transaction in block.
// All changes are written in a single LevelDB batch for consistency.
func (ws *WorldState) ApplyBlock(block types.Block) error {
	batch := new(leveldb.Batch)

	for _, tx := range block.Transactions {
		// Spend (delete) each referenced input.
		for _, vin := range tx.Vin {
			batch.Delete([]byte(utxoKey(vin.Txid, vin.Vout)))
		}

		// Create (put) each new output.
		for _, vout := range tx.Vout {
			val, err := json.Marshal(vout)
			if err != nil {
				return fmt.Errorf("world state: marshal vout %d of tx %s: %w",
					vout.N, tx.Txid, err)
			}
			batch.Put([]byte(utxoKey(tx.Txid, vout.N)), val)
		}
	}

	if err := ws.db.Write(batch, nil); err != nil {
		return fmt.Errorf("world state: write batch: %w", err)
	}
	return nil
}

// GetUTXO looks up a single unspent output by (txid, vout index).
// Returns leveldb.ErrNotFound if the output has been spent or never existed.
func (ws *WorldState) GetUTXO(txid string, vout int) (*types.VOUT, error) {
	data, err := ws.db.Get([]byte(utxoKey(txid, vout)), nil)
	if err != nil {
		return nil, err
	}
	var v types.VOUT
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("world state: unmarshal vout: %w", err)
	}
	return &v, nil
}

// AllUTXOs returns every unspent output currently in the world state.
func (ws *WorldState) AllUTXOs() ([]UTXOEntry, error) {
	iter := ws.db.NewIterator(util.BytesPrefix([]byte("utxo:")), nil)
	defer iter.Release()

	var entries []UTXOEntry
	for iter.Next() {
		var v types.VOUT
		if err := json.Unmarshal(iter.Value(), &v); err != nil {
			return nil, fmt.Errorf("world state: unmarshal utxo entry: %w", err)
		}
		// key format: utxo:<txid>:<n>
		var txid string
		var n int
		fmt.Sscanf(string(iter.Key()), "utxo:%64s", &txid)
		// Use the N field from the stored VOUT directly — it is authoritative.
		n = v.N
		entries = append(entries, UTXOEntry{Txid: txid, Index: n, Out: v})
	}
	return entries, iter.Error()
}

// UTXOCount returns the total number of unspent outputs in the world state.
func (ws *WorldState) UTXOCount() (int, error) {
	iter := ws.db.NewIterator(util.BytesPrefix([]byte("utxo:")), nil)
	defer iter.Release()
	n := 0
	for iter.Next() {
		n++
	}
	return n, iter.Error()
}

// Close closes the underlying LevelDB handle.
func (ws *WorldState) Close() error {
	return ws.db.Close()
}

func utxoKey(txid string, vout int) string {
	return fmt.Sprintf("utxo:%s:%d", txid, vout)
}
