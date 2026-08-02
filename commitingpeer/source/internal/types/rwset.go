package types

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// KVRead records a key read during Core WASM simulation.
type KVRead struct {
	Key     string `json:"key"`
	Version string `json:"version,omitempty"`
	Value   string `json:"value,omitempty"` // hex
}

// KVWrite records a key write/delete from simulation.
type KVWrite struct {
	Key      string `json:"key"`
	Value    string `json:"value,omitempty"` // hex
	IsDelete bool   `json:"is_delete,omitempty"`
}

// RWSet is applied on Commit Peer ApplyBlock (KV prefix kv:).
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

// CanonicalBytes returns sha256 of sorted JSON (same as coreservice).
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
