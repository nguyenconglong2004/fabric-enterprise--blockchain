package storage

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

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

// Validation codes for per-tx apply (Fabric-style).
const (
	TxValid       = "VALID"
	TxInvalidMVCC = "INVALID_MVCC"
)

// TxApplyResult is the MVCC outcome for one transaction in a block.
type TxApplyResult struct {
	Txid   string
	Index  int
	Code   string
	Reason string
}

// WorldState is account/KV world state backed by LevelDB.
//
// Key schema:
//
//	kv:<key>  →  raw value bytes (from tx.rw_set writes), e.g. balance:<addr>
//	ver:<key> →  version string ("<blockHeight>:<txIndex>" or "admin:<n>")
//
// ApplyBlock checks read-set versions (MVCC) then applies writes. Legacy utxo:* keys are ignored.
type WorldState struct {
	db       *leveldb.DB
	adminSeq atomic.Uint64
}

// NewWorldState opens (or creates) a LevelDB database at path.
func NewWorldState(path string) (*WorldState, error) {
	db, err := leveldb.OpenFile(path, nil)
	if err != nil {
		return nil, fmt.Errorf("world state: open leveldb at %q: %w", path, err)
	}
	return &WorldState{db: db}, nil
}

// ApplyBlock runs MVCC per tx (in order), applies valid write-sets, bumps versions.
// blockHeight is 1-based local height used in version strings.
func (ws *WorldState) ApplyBlock(block types.Block, blockHeight int64) ([]TxApplyResult, error) {
	batch := new(leveldb.Batch)
	results := make([]TxApplyResult, 0, len(block.Transactions))

	// Overlay of versions after successful applies in this block.
	liveVer := make(map[string]string)

	getVer := func(key string) (string, error) {
		if v, ok := liveVer[key]; ok {
			return v, nil
		}
		v, err := ws.GetVersion(key)
		if err != nil {
			return "", err
		}
		liveVer[key] = v
		return v, nil
	}

	for i, tx := range block.Transactions {
		res := TxApplyResult{Txid: tx.Txid, Index: i, Code: TxValid}
		if tx.RWSet == nil {
			results = append(results, res)
			continue
		}

		conflictKey, err := checkReadSet(tx.RWSet.Reads, getVer)
		if err != nil {
			return nil, fmt.Errorf("world state: mvcc tx=%s: %w", tx.Txid, err)
		}
		if conflictKey != "" {
			res.Code = TxInvalidMVCC
			res.Reason = "read-set version mismatch on key " + conflictKey
			results = append(results, res)
			continue
		}

		ver := fmt.Sprintf("%d:%d", blockHeight, i)
		for _, w := range tx.RWSet.Writes {
			k := kvKey(w.Key)
			vk := verKey(w.Key)
			if w.IsDelete {
				batch.Delete([]byte(k))
				batch.Delete([]byte(vk))
				liveVer[w.Key] = ""
				continue
			}
			raw, err := w.ValueBytes()
			if err != nil {
				return nil, fmt.Errorf("world state: bad rw_set value hex key=%s tx=%s: %w", w.Key, tx.Txid, err)
			}
			batch.Put([]byte(k), raw)
			batch.Put([]byte(vk), []byte(ver))
			liveVer[w.Key] = ver
		}
		results = append(results, res)
	}

	if err := ws.db.Write(batch, nil); err != nil {
		return nil, fmt.Errorf("world state: write batch: %w", err)
	}
	return results, nil
}

// checkReadSet returns conflicting key if any read version != current, else "".
func checkReadSet(reads []types.KVRead, getVer func(string) (string, error)) (conflictKey string, err error) {
	for _, r := range reads {
		cur, err := getVer(r.Key)
		if err != nil {
			return "", err
		}
		if cur != r.Version {
			return r.Key, nil
		}
	}
	return "", nil
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

// PutBalance writes kv balance:<addr> as decimal ASCII (bumps version).
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

func verKey(key string) string {
	return "ver:" + key
}

// GetKV returns committed contract state for key. ErrNotFound if missing.
func (ws *WorldState) GetKV(key string) ([]byte, error) {
	return ws.db.Get([]byte(kvKey(key)), nil)
}

// GetVersion returns the MVCC version for key. Missing key / missing ver → "".
func (ws *WorldState) GetVersion(key string) (string, error) {
	raw, err := ws.db.Get([]byte(verKey(key)), nil)
	if err == leveldb.ErrNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// GetKVWithVersion returns value + version. found=false if kv missing.
func (ws *WorldState) GetKVWithVersion(key string) (val []byte, version string, found bool, err error) {
	val, err = ws.GetKV(key)
	if err == leveldb.ErrNotFound {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	version, err = ws.GetVersion(key)
	if err != nil {
		return nil, "", false, err
	}
	return val, version, true, nil
}

func (ws *WorldState) nextAdminVersion() string {
	n := ws.adminSeq.Add(1)
	return fmt.Sprintf("admin:%d:%d", time.Now().UnixNano(), n)
}

// PutKV writes a KV entry and bumps its version (admin/mint path).
func (ws *WorldState) PutKV(key string, value []byte) error {
	ver := ws.nextAdminVersion()
	batch := new(leveldb.Batch)
	batch.Put([]byte(kvKey(key)), value)
	batch.Put([]byte(verKey(key)), []byte(ver))
	if err := ws.db.Write(batch, nil); err != nil {
		return fmt.Errorf("world state: put kv: %w", err)
	}
	return nil
}

// DeleteKV removes value + version (tests / admin).
func (ws *WorldState) DeleteKV(key string) error {
	batch := new(leveldb.Batch)
	batch.Delete([]byte(kvKey(key)))
	batch.Delete([]byte(verKey(key)))
	return ws.db.Write(batch, nil)
}
