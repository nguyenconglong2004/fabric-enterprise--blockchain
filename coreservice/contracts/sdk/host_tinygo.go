//go:build tinygo

package sdk

//go:wasmimport env PutState
func putState(keyPtr, keySize, valPtr, valSize uint32) uint32

//go:wasmimport env GetState
func getState(keyPtr, keySize, outPtr, outCap uint32) uint32
