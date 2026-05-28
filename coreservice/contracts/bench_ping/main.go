// bench_ping — minimal contract for throughput benchmarks (one field, no PutState).
package main

import (
	"encoding/json"
	"unsafe"
)

type Payload struct {
	V string `json:"v"`
}

//export allocate
func allocate(size uint32) *byte {
	buf := make([]byte, size)
	return &buf[0]
}

//export verify_tx
func verify_tx(ptr uint32, size uint32) uint32 {
	if size == 0 || size > 512 {
		return 0
	}
	payloadBytes := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), size)

	var p Payload
	if err := json.Unmarshal(payloadBytes, &p); err != nil {
		return 0
	}
	if len(p.V) == 0 || len(p.V) > 128 {
		return 0
	}
	return 1
}

func main() {}
