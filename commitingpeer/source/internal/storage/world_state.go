package storage

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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

// WorldState is account/KV world state backed by LevelDB.
//
// Key schema:
//
//	kv:<key>  →  raw value bytes (from tx.rw_set writes), e.g. balance:<addr>
//
// ApplyBlock only applies rw_set writes (account model). Legacy utxo:* keys are ignored.
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

// ApplyBlock applies each tx's rw_set writes into kv:<key>.
func (ws *WorldState) ApplyBlock(block types.Block) error {
	batch := new(leveldb.Batch)

	for _, tx := range block.Transactions {
		if tx.RWSet == nil {
			continue
		}
		for _, w := range tx.RWSet.Writes {
			k := kvKey(w.Key)
			if w.IsDelete {
				batch.Delete([]byte(k))
				continue
			}
			raw, err := w.ValueBytes()
			if err != nil {
				return fmt.Errorf("world state: bad rw_set value hex key=%s tx=%s: %w", w.Key, tx.Txid, err)
			}
			batch.Put([]byte(k), raw)
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
		key := string(iter.Key())
		// key: utxo:<txid>:<n>
		parts := strings.Split(key, ":")
		if len(parts) >= 3 {
			txid = parts[1]
			fmt.Sscanf(parts[len(parts)-1], "%d", &n)
		}
		// Prefer N from stored VOUT.
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

// PutUTXO writes (or overwrites) a single UTXO entry — used by faucet/mint for demo accounts.
func (ws *WorldState) PutUTXO(txid string, out types.VOUT) error {
	val, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("world state: marshal vout: %w", err)
	}
	if err := ws.db.Put([]byte(utxoKey(txid, out.N)), val, nil); err != nil {
		return fmt.Errorf("world state: put utxo: %w", err)
	}
	return nil
}

// UTXOsByAddress returns unspent outputs whose ScriptPubKey.Addresses contain addr.
func (ws *WorldState) UTXOsByAddress(addr string) ([]UTXOEntry, error) {
	all, err := ws.AllUTXOs()
	if err != nil {
		return nil, err
	}
	var out []UTXOEntry
	for _, e := range all {
		for _, a := range e.Out.ScriptPubKey.Addresses {
			if a == addr {
				out = append(out, e)
				break
			}
		}
	}
	return out, nil
}

// BalanceByAddress is deprecated (UTXO sum). Prefer GetBalance (KV account model).
func (ws *WorldState) BalanceByAddress(addr string) (int64, error) {
	return ws.GetBalance(addr)
}

// GetBalance reads kv balance:<addr> (decimal ASCII). Missing key → 0.
func (ws *WorldState) GetBalance(addr string) (int64, error) {
	addr = strings.TrimSpace(strings.ToLower(addr))
	raw, err := ws.GetKV("balance:" + addr)
	if err == leveldb.ErrNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

// PutBalance writes kv balance:<addr> as decimal ASCII.
func (ws *WorldState) PutBalance(addr string, bal int64) error {
	addr = strings.TrimSpace(strings.ToLower(addr))
	return ws.PutKV("balance:"+addr, []byte(strconv.FormatInt(bal, 10)))
}

// Close closes the underlying LevelDB handle.
func (ws *WorldState) Close() error {
	return ws.db.Close()
}

func utxoKey(txid string, vout int) string {
	return fmt.Sprintf("utxo:%s:%d", txid, vout)
}

func kvKey(key string) string {
	return "kv:" + key
}

// GetKV returns committed contract state for key. ErrNotFound if missing.
func (ws *WorldState) GetKV(key string) ([]byte, error) {
	return ws.db.Get([]byte(kvKey(key)), nil)
}

// PutKV writes a KV entry (tests / admin).
func (ws *WorldState) PutKV(key string, value []byte) error {
	return ws.db.Put([]byte(kvKey(key)), value, nil)
}
