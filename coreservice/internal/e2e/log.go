package e2e

import (
	"os"
	"strings"
)

// LogEnabled controls [e2e] logs on Core (set E2E_LOG=0 to disable). Commit peer always logs block SoT summary.
func LogEnabled() bool {
	v := strings.TrimSpace(os.Getenv("E2E_LOG"))
	if v == "" {
		return true
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}
