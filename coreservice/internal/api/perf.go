package api

import (
	"os"
	"strings"
)

// asyncEndorse returns true unless CORE_ASYNC_ENDORSE=0 (default: async after sign).
func asyncEndorse() bool {
	v := strings.TrimSpace(os.Getenv("CORE_ASYNC_ENDORSE"))
	return v == "" || v == "1" || strings.EqualFold(v, "true")
}

// endorseLeaderOnly skips dialing every follower when leader send fails (set CORE_ENDORSE_FALLBACK=1 to retry all).
func endorseLeaderOnly() bool {
	return strings.TrimSpace(os.Getenv("CORE_ENDORSE_FALLBACK")) != "1"
}
