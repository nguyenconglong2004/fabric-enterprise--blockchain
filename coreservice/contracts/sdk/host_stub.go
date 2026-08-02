//go:build !tinygo

package sdk

// Stubs for gopls / `go test` outside TinyGo. Real host calls are in host_tinygo.go.
func putState(keyPtr, keySize, valPtr, valSize uint32) uint32 { return 0 }
func getState(keyPtr, keySize, outPtr, outCap uint32) uint32  { return 0 }
