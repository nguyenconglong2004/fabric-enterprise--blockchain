package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HandleBalanceStream SSE-polls wallet balance for an address.
// GET /api/wallet/balance/stream?address=...&token=...
// Prefer explicit address= (matches Profile UI); token is fallback to resolve address.
func (s *APIServer) HandleBalanceStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	addr := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("address")))
	tok := strings.TrimSpace(r.URL.Query().Get("token"))
	if tok == "" {
		tok = bearerToken(r)
	}
	var discount float64
	var username string

	// Prefer address from query (FE pins the wallet being viewed).
	if addr != "" && s.DB != nil {
		if acc, err := s.DB.GetAccountByAddress(addr); err == nil && acc != nil {
			discount = acc.Discount
			username = acc.Username
			addr = strings.ToLower(acc.Address)
		}
	}
	if addr == "" && tok != "" && s.DB != nil {
		acc, err := s.DB.GetSessionAccount(tok)
		if err == nil && acc != nil {
			addr = strings.ToLower(acc.Address)
			discount = acc.Discount
			username = acc.Username
		} else {
			// tokenCandidates path (query / headers)
			if a, err := s.accountFromRequest(r); err == nil && a != nil {
				addr = strings.ToLower(a.Address)
				discount = a.Discount
				username = a.Username
			}
		}
	}
	if addr == "" {
		http.Error(w, "need address or token", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	send := func(eventType string, payload map[string]interface{}) {
		b, _ := json.Marshal(payload)
		fmt.Fprintf(w, "event: %s\n", eventType)
		fmt.Fprintf(w, "data: %s\n\n", string(b))
		flusher.Flush()
	}

	send("ready", map[string]interface{}{
		"status":   "connected",
		"address":  addr,
		"username": username,
		"discount": discount,
	})

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastBal int64 = -1
	push := func(force bool) {
		bal, err := s.fetchBalance(addr)
		if err != nil {
			send("balance_error", map[string]interface{}{
				"message": err.Error(),
				"address": addr,
			})
			return
		}
		if !force && bal == lastBal {
			fmt.Fprintf(w, ": ping %d bal=%d\n\n", time.Now().Unix(), bal)
			flusher.Flush()
			return
		}
		lastBal = bal
		send("balance", map[string]interface{}{
			"address":    addr,
			"username":   username,
			"discount":   discount,
			"balance":    bal,
			"updated_at": time.Now().UnixMilli(),
		})
	}
	push(true)

	n := 0
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			n++
			// Re-emit absolute balance every ~2s so late UI / missed events still sync.
			push(n%4 == 0)
		}
	}
}
