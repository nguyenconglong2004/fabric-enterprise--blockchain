// demo_inventory — contract mẫu thứ hai: payload JSON {op, sku, qty}
// Build: tinygo build -o my_contract.wasm -target wasm -no-debug -scheduler=none .
package main

import (
	"encoding/json"
	"unsafe"
)

type Payload struct {
	Op  string `json:"op"`
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

//go:wasmimport env PutState
func PutState(keyPtr, keySize, valPtr, valSize uint32) uint32

//export allocate
func allocate(size uint32) *byte {
	buf := make([]byte, size)
	return &buf[0]
}

//export verify_tx
func verify_tx(ptr uint32, size uint32) uint32 {
	payloadBytes := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), size)

	var p Payload
	if err := json.Unmarshal(payloadBytes, &p); err != nil {
		return 0
	}
	if p.Op != "register" || p.SKU == "" || p.Qty < 0 {
		return 0
	}

	keyStr := "Inv_" + p.SKU
	keyBytes := []byte(keyStr)
	valBytes := payloadBytes

	kPtr := uint32(uintptr(unsafe.Pointer(&keyBytes[0])))
	kSize := uint32(len(keyBytes))
	vPtr := uint32(uintptr(unsafe.Pointer(&valBytes[0])))
	vSize := uint32(len(valBytes))

	if PutState(kPtr, kSize, vPtr, vSize) == 1 {
		return 1
	}
	return 0
}

func main() {}
