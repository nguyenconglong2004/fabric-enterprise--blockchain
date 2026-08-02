package metrics

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"commiting-peer/internal/storage"

	"github.com/syndtr/goleveldb/leveldb"
)

// WalletHandler exposes mint / balance / state against KV world state (account model).
type WalletHandler struct {
	WS *storage.WorldState
}

func (h *WalletHandler) Register(mux *http.ServeMux) {
	if h == nil || h.WS == nil {
		return
	}
	mux.HandleFunc("/wallet/mint", h.handleMint)
	mux.HandleFunc("/wallet/balance", h.handleBalance)
	mux.HandleFunc("/wallet/state", h.handleGetState)
}

type mintReq struct {
	Address  string  `json:"address"`
	Amount   int64   `json:"amount"`
	Discount float64 `json:"discount"` // optional; stored at discount:<addr>
	Set      bool    `json:"set"`      // if true, set balance; else add
}

func discountKey(addr string) string { return "discount:" + addr }

func (h *WalletHandler) handleMint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "only POST")
		return
	}
	var req mintReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	addr := strings.TrimSpace(strings.ToLower(req.Address))
	if len(addr) != 40 {
		writeErr(w, http.StatusBadRequest, "address must be 40-char hex (P2PKH)")
		return
	}
	if req.Amount < 0 {
		writeErr(w, http.StatusBadRequest, "amount must be >= 0")
		return
	}

	var newBal int64
	if req.Set {
		newBal = req.Amount
	} else {
		cur, err := h.WS.GetBalance(addr)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		newBal = cur + req.Amount
	}
	if err := h.WS.PutBalance(addr, newBal); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Discount > 0 || req.Set {
		_ = h.WS.PutKV(discountKey(addr), []byte(strconv.FormatFloat(req.Discount, 'f', 6, 64)))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"address":  addr,
		"balance":  newBal,
		"discount": req.Discount,
	})
}

func (h *WalletHandler) handleBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "only GET")
		return
	}
	addr := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("address")))
	if addr == "" {
		writeErr(w, http.StatusBadRequest, "missing address")
		return
	}
	bal, err := h.WS.GetBalance(addr)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"address": addr,
		"balance": bal,
	})
}

// GET /wallet/state?key=... — committed KV from rw_set apply / mint.
func (h *WalletHandler) handleGetState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "only GET")
		return
	}
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	if key == "" {
		writeErr(w, http.StatusBadRequest, "missing key")
		return
	}
	val, err := h.WS.GetKV(key)
	if err == leveldb.ErrNotFound {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"key":   key,
			"found": false,
		})
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"key":   key,
		"found": true,
		"value": hex.EncodeToString(val),
	})
}