// Package sdk is the TinyGo guest-side host ABI for Core WASM contracts.
//
// Importing this package exports `allocate` for the Core host (do not re-export it in main).
// Engineers implement in their main package:
//
//	//export verify_tx   — validate payload / business rules (prefer no writes)
//	//export execute     — side effects via PutState after verify_tx succeeds
//
// Build with: tinygo build -o my_contract.wasm -target wasi -no-debug -scheduler=none .
package sdk

import "unsafe"

// PutState writes a key/value into Core's local ledger LevelDB. Returns true on success.
func PutState(key, value []byte) bool {
	if len(key) == 0 {
		return false
	}
	kPtr := uint32(uintptr(unsafe.Pointer(&key[0])))
	var vPtr uint32
	if len(value) > 0 {
		vPtr = uint32(uintptr(unsafe.Pointer(&value[0])))
	}
	return putState(kPtr, uint32(len(key)), vPtr, uint32(len(value))) == 1
}

// GetState reads a key from Core ledger. ok=false if missing or buffer too small.
// Pass empty out to probe size (host returns length when outCap==0).
func GetState(key, out []byte) (n uint32, ok bool) {
	if len(key) == 0 {
		return 0, false
	}
	kPtr := uint32(uintptr(unsafe.Pointer(&key[0])))
	var outPtr uint32
	if len(out) > 0 {
		outPtr = uint32(uintptr(unsafe.Pointer(&out[0])))
	}
	n = getState(kPtr, uint32(len(key)), outPtr, uint32(len(out)))
	if n == 0 {
		return 0, false
	}
	if len(out) == 0 {
		return n, true
	}
	if n > uint32(len(out)) {
		return 0, false
	}
	return n, true
}

// SizeOf returns the stored value length for key, or 0 if missing.
func SizeOf(key []byte) uint32 {
	if len(key) == 0 {
		return 0
	}
	kPtr := uint32(uintptr(unsafe.Pointer(&key[0])))
	return getState(kPtr, uint32(len(key)), 0, 0)
}

// Allocate reserves size bytes in guest linear memory and returns a pointer
// the host can Write into. Prefer relying on the exported allocate below;
// this helper remains for rare custom allocators.
func Allocate(size uint32) *byte {
	buf := make([]byte, size)
	return &buf[0]
}

//export allocate
func allocate(size uint32) *byte {
	return Allocate(size)
}

// PayloadSlice maps host-written linear memory into a Go byte slice.
func PayloadSlice(ptr, size uint32) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), size)
}
