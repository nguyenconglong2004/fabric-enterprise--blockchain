package types

import (
	"crypto/sha256"
	"encoding/json"
	"sort"
)

// KVRead / KVWrite / RWSet — passthrough from Core (applied on Commit Peer).
type KVRead struct {
	Key     string `json:"key"`
	Version string `json:"version,omitempty"`
	Value   string `json:"value,omitempty"`
}

type KVWrite struct {
	Key      string `json:"key"`
	Value    string `json:"value,omitempty"`
	IsDelete bool   `json:"is_delete,omitempty"`
}

type RWSet struct {
	Reads  []KVRead  `json:"reads,omitempty"`
	Writes []KVWrite `json:"writes,omitempty"`
}

// CanonicalBytes matches coreservice / committingpeer (for any local verify).
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
