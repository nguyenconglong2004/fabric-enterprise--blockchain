package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// KVRead records a key read during WASM simulation.
type KVRead struct {
	Key     string `json:"key"`
	Version string `json:"version,omitempty"`
	Value   string `json:"value,omitempty"` // hex; optional snapshot at read time
}

// KVWrite records a key write (or delete) during WASM simulation.
type KVWrite struct {
	Key      string `json:"key"`
	Value    string `json:"value,omitempty"` // hex; empty if IsDelete
	IsDelete bool   `json:"is_delete,omitempty"`
}

// RWSet is the Fabric-style read/write set collected during Core simulate.
// Writes are applied on Commit Peer ApplyBlock (not on Core LevelDB).
type RWSet struct {
	Reads  []KVRead  `json:"reads,omitempty"`
	Writes []KVWrite `json:"writes,omitempty"`
}

// ValueBytes decodes hex Value.
func (w KVWrite) ValueBytes() ([]byte, error) {
	if w.Value == "" {
		return nil, nil
	}
	return hex.DecodeString(w.Value)
}

// PutWrite upserts a write by key (last write wins within the tx).
func (rw *RWSet) PutWrite(key string, val []byte) {
	if rw == nil {
		return
	}
	hexVal := hex.EncodeToString(val)
	for i := range rw.Writes {
		if rw.Writes[i].Key == key {
			rw.Writes[i].Value = hexVal
			rw.Writes[i].IsDelete = false
			return
		}
	}
	rw.Writes = append(rw.Writes, KVWrite{Key: key, Value: hexVal})
}

// DeleteWrite marks a key deleted in the write set.
func (rw *RWSet) DeleteWrite(key string) {
	if rw == nil {
		return
	}
	for i := range rw.Writes {
		if rw.Writes[i].Key == key {
			rw.Writes[i].Value = ""
			rw.Writes[i].IsDelete = true
			return
		}
	}
	rw.Writes = append(rw.Writes, KVWrite{Key: key, IsDelete: true})
}

// LookupWrite returns value from write-set overlay (ok=false if not written).
func (rw *RWSet) LookupWrite(key string) (val []byte, deleted bool, ok bool) {
	if rw == nil {
		return nil, false, false
	}
	for i := range rw.Writes {
		if rw.Writes[i].Key != key {
			continue
		}
		if rw.Writes[i].IsDelete {
			return nil, true, true
		}
		b, err := hex.DecodeString(rw.Writes[i].Value)
		if err != nil {
			return nil, false, false
		}
		return b, false, true
	}
	return nil, false, false
}

// RecordRead appends a read if key not already recorded.
func (rw *RWSet) RecordRead(key string, val []byte) {
	if rw == nil {
		return
	}
	for _, r := range rw.Reads {
		if r.Key == key {
			return
		}
	}
	entry := KVRead{Key: key}
	if len(val) > 0 {
		entry.Value = hex.EncodeToString(val)
	}
	rw.Reads = append(rw.Reads, entry)
}

// CanonicalBytes returns a stable digest input for endorsement (sorted keys).
// Empty/nil RWSet → empty slice (compatible with txs that do not touch KV).
func (rw *RWSet) CanonicalBytes() []byte {
	if rw == nil || (len(rw.Reads) == 0 && len(rw.Writes) == 0) {
		return nil
	}
	type wire struct {
		Reads  []KVRead  `json:"reads,omitempty"`
		Writes []KVWrite `json:"writes,omitempty"`
	}
	w := wire{
		Reads:  append([]KVRead(nil), rw.Reads...),
		Writes: append([]KVWrite(nil), rw.Writes...),
	}
	sort.Slice(w.Reads, func(i, j int) bool { return w.Reads[i].Key < w.Reads[j].Key })
	sort.Slice(w.Writes, func(i, j int) bool { return w.Writes[i].Key < w.Writes[j].Key })
	b, err := json.Marshal(w)
	if err != nil {
		return nil
	}
	sum := sha256.Sum256(b)
	return sum[:]
}
