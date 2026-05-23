package vm

import "os"

// Verbose enables hot-path logging and WASM stdout (set CORE_LOG=debug).
func Verbose() bool {
	return os.Getenv("CORE_LOG") == "debug"
}
