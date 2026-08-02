package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"coreservice/internal/storage"
	"coreservice/internal/wallet"

	"golang.org/x/crypto/bcrypt"
)

const sessionTTL = 24 * time.Hour

func (s *APIServer) commitWalletBase() string {
	base := strings.TrimSpace(os.Getenv("COMMIT_PEER_METRICS_URL"))
	if base == "" {
		base = strings.TrimSpace(os.Getenv("COMMIT_PEER_METRICS_HTTP"))
	}
	if base == "" {
		base = "http://127.0.0.1:8081"
	}
	return strings.TrimRight(base, "/")
}

// commitPeerHTTPErr explains common failures (e.g. old commit peer without /wallet/*).
// Note: plain "404 page not found" starts with digits — json.Decoder would otherwise
// report "cannot unmarshal number into Go value of type struct".
func commitPeerHTTPErr(op string, status int, raw []byte) error {
	body := strings.TrimSpace(string(raw))
	if status == http.StatusNotFound || strings.Contains(body, "404 page not found") {
		return fmt.Errorf("%s: commit peer missing /wallet/* (HTTP %d) — chạy peer từ Thesis repo, không phải bản clone cũ", op, status)
	}
	var m map[string]string
	if json.Unmarshal(raw, &m) == nil && m["error"] != "" {
		return fmt.Errorf("%s: %s", op, m["error"])
	}
	if body == "" {
		return fmt.Errorf("%s: HTTP %d", op, status)
	}
	if len(body) > 200 {
		body = body[:200] + "…"
	}
	return fmt.Errorf("%s: HTTP %d: %s", op, status, body)
}

func (s *APIServer) mintBalance(address string, amount int64, discount float64) error {
	body, _ := json.Marshal(map[string]interface{}{
		"address":  address,
		"amount":   amount,
		"discount": discount,
		"set":      true, // idempotent seed: set absolute balance
	})
	resp, err := http.Post(s.commitWalletBase()+"/wallet/mint", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mint call: %w (is commit peer :8081 up?)", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return commitPeerHTTPErr("mint failed", resp.StatusCode, raw)
	}
	return nil
}

func (s *APIServer) fetchBalance(address string) (int64, error) {
	u := s.commitWalletBase() + "/wallet/balance?address=" + url.QueryEscape(address)
	resp, err := http.Get(u)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, err
	}
	if resp.StatusCode >= 300 {
		return 0, commitPeerHTTPErr("balance", resp.StatusCode, raw)
	}
	var out struct {
		Balance int64 `json:"balance"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, fmt.Errorf("balance: bad JSON from commit peer: %w (body=%q)", err, truncate(string(raw), 120))
	}
	return out.Balance, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// SeedDemoAccounts creates alice/bob/charlie if missing and sets KV balances on commit peer.
func (s *APIServer) SeedDemoAccounts() {
	if s.DB == nil {
		return
	}
	if err := s.DB.EnsureAccountsSchema(); err != nil {
		fmt.Printf("⚠️  accounts schema: %v\n", err)
		return
	}
	type seed struct {
		user, pass string
		discount   float64
		balance    int64
	}
	seeds := []seed{
		{"alice", "password123", 0.10, 1000},
		{"bob", "password123", 0.00, 1000},
		{"charlie", "password123", 0.05, 500},
	}
	for _, sd := range seeds {
		existing, err := s.DB.GetAccountByUsername(sd.user)
		if err != nil {
			fmt.Printf("⚠️  seed %s: %v\n", sd.user, err)
			continue
		}
		if existing != nil {
			// Do NOT re-mint: set:true would wipe on-chain balance every Core restart.
			fmt.Printf("👤 Account sẵn: %s address=%s\n", sd.user, existing.Address)
			continue
		}
		seedBytes, _, pub, err := wallet.NewKeypair()
		if err != nil {
			fmt.Printf("⚠️  keygen %s: %v\n", sd.user, err)
			continue
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(sd.pass), bcrypt.DefaultCost)
		if err != nil {
			fmt.Printf("⚠️  bcrypt %s: %v\n", sd.user, err)
			continue
		}
		acc := &storage.Account{
			Username:       sd.user,
			PasswordHash:   string(hash),
			Address:        wallet.AddressFromPub(pub),
			PubkeyHex:      hex.EncodeToString(pub),
			SeedHex:        hex.EncodeToString(seedBytes),
			Discount:       sd.discount,
			InitialBalance: sd.balance,
		}
		if err := s.DB.CreateAccount(acc); err != nil {
			fmt.Printf("⚠️  create %s: %v\n", sd.user, err)
			continue
		}
		if err := s.mintBalance(acc.Address, sd.balance, sd.discount); err != nil {
			fmt.Printf("⚠️  mint %s: %v\n", sd.user, err)
		} else {
			fmt.Printf("👤 Seeded %s address=%s balance=%d (pass=%s)\n",
				sd.user, acc.Address, sd.balance, sd.pass)
		}
	}
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func bearerToken(r *http.Request) string {
	cands := tokenCandidates(r)
	if len(cands) == 0 {
		return ""
	}
	return cands[0]
}

// tokenCandidates collects auth tokens from headers/query (deduped).
// Tries each until a valid session is found — Vite/proxy sometimes mangles Authorization on POST.
func tokenCandidates(r *http.Request) []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(t string) {
		t = strings.TrimSpace(t)
		t = strings.Trim(t, `"'`)
		if len(t) >= 7 && strings.EqualFold(t[:6], "bearer") && t[6] == ' ' {
			t = strings.TrimSpace(t[7:])
		}
		if t == "" || t == "undefined" || t == "null" {
			return
		}
		if _, ok := seen[t]; ok {
			return
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}

	// Prefer ?token= first — FE controls it explicitly; Authorization is often
	// stale/mangled by proxies and caused "wrong account on reload".
	add(r.URL.Query().Get("token"))
	add(r.Header.Get("X-Auth-Token"))
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(h) >= 7 && strings.EqualFold(h[:6], "bearer") && h[6] == ' ' {
		add(h[7:])
	}
	return out
}

func (s *APIServer) accountFromRequest(r *http.Request) (*storage.Account, error) {
	cands := tokenCandidates(r)
	if len(cands) == 0 {
		return nil, fmt.Errorf("missing auth token (Authorization / X-Auth-Token / ?token=)")
	}
	if s.DB == nil {
		return nil, fmt.Errorf("database unavailable")
	}
	var lastErr error
	for _, tok := range cands {
		acc, err := s.DB.GetSessionAccount(tok)
		if err != nil {
			lastErr = err
			continue
		}
		if acc != nil {
			return acc, nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("invalid or expired session — sign in again")
}

func (s *APIServer) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if s.DB == nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	acc, err := s.DB.GetAccountByUsername(strings.TrimSpace(body.Username))
	if err != nil || acc == nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(acc.PasswordHash), []byte(body.Password)) != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	tok, err := randomToken()
	if err != nil {
		http.Error(w, "token error", http.StatusInternalServerError)
		return
	}
	exp := time.Now().UTC().Add(sessionTTL)
	// One active session per account — avoids reload picking an older token/user.
	_ = s.DB.DeleteSessionsForAccount(acc.ID)
	if err := s.DB.CreateSession(tok, acc.ID, exp); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	bal, _ := s.fetchBalance(acc.Address)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"token":      tok,
		"expires_at": exp,
		"account": map[string]interface{}{
			"username": acc.Username,
			"address":  acc.Address,
			"discount": acc.Discount,
			"balance":  bal,
		},
	})
}

func (s *APIServer) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	tok := bearerToken(r)
	if tok != "" && s.DB != nil {
		_ = s.DB.DeleteSession(tok)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *APIServer) HandleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	acc, err := s.accountFromRequest(r)
	if err != nil || acc == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	bal, _ := s.fetchBalance(acc.Address)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"username": acc.Username,
		"address":  acc.Address,
		"discount": acc.Discount,
		"balance":  bal,
		"pubkey":   acc.PubkeyHex,
	})
}

func (s *APIServer) HandleBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	addr := strings.TrimSpace(r.URL.Query().Get("address"))
	if addr == "" {
		acc, err := s.accountFromRequest(r)
		if err != nil || acc == nil {
			http.Error(w, "need address or auth", http.StatusUnauthorized)
			return
		}
		addr = acc.Address
	}
	bal, err := s.fetchBalance(addr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"address": addr,
		"balance": bal,
	})
}
