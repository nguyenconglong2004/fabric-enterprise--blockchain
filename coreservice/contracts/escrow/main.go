// escrow — multi-step hold: lock funds, then release to beneficiary or refund sender.
//
// KV:
//   escrow:<id>        = JSON {from,to,amount} while locked
//   escrow_receipt:<id> = last action payload (audit)
package main

import (
	"encoding/json"
	"strconv"

	"fabricwasm/sdk"
)

type Payload struct {
	ID     string `json:"id"`
	Action string `json:"action"` // lock | release | refund
	From   string `json:"from"`
	To     string `json:"to"`
	Amount int64  `json:"amount"`
	Memo   string `json:"memo" schema:"optional"`
}

type escrowRec struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Amount int64  `json:"amount"`
}

func balKey(addr string) []byte    { return []byte("balance:" + addr) }
func escrowKey(id string) []byte   { return []byte("escrow:" + id) }
func receiptKey(id string) []byte  { return []byte("escrow_receipt:" + id) }

func getInt(key []byte) (int64, bool) {
	n := sdk.SizeOf(key)
	if n == 0 {
		return 0, true
	}
	buf := make([]byte, n)
	got, ok := sdk.GetState(key, buf)
	if !ok || got == 0 {
		return 0, true
	}
	v, err := strconv.ParseInt(string(buf[:got]), 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func putInt(key []byte, v int64) bool {
	return sdk.PutState(key, []byte(strconv.FormatInt(v, 10)))
}

func loadEscrow(id string) (escrowRec, bool) {
	key := escrowKey(id)
	n := sdk.SizeOf(key)
	if n == 0 {
		return escrowRec{}, false
	}
	buf := make([]byte, n)
	got, ok := sdk.GetState(key, buf)
	if !ok || got == 0 {
		return escrowRec{}, false
	}
	var e escrowRec
	if err := json.Unmarshal(buf[:got], &e); err != nil {
		return escrowRec{}, false
	}
	if e.Amount <= 0 || len(e.From) != 40 || len(e.To) != 40 {
		return escrowRec{}, false
	}
	return e, true
}

func validAction(a string) bool {
	return a == "lock" || a == "release" || a == "refund"
}

//export verify_tx
func verify_tx(ptr uint32, size uint32) uint32 {
	if size == 0 || size > 8192 {
		return 0
	}
	var p Payload
	if err := json.Unmarshal(sdk.PayloadSlice(ptr, size), &p); err != nil {
		return 0
	}
	if p.ID == "" || len(p.ID) > 64 || !validAction(p.Action) {
		return 0
	}
	if len(p.Memo) > 200 {
		return 0
	}
	switch p.Action {
	case "lock":
		if len(p.From) != 40 || len(p.To) != 40 || p.Amount <= 0 {
			return 0
		}
		if p.From == p.To {
			return 0
		}
	case "release", "refund":
		// parties/amount taken from stored escrow; payload.from must be present (session)
		if len(p.From) != 40 {
			return 0
		}
	}
	return 1
}

//export execute
func execute(ptr uint32, size uint32) uint32 {
	raw := sdk.PayloadSlice(ptr, size)
	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return 0
	}
	if p.ID == "" || !validAction(p.Action) {
		return 0
	}

	switch p.Action {
	case "lock":
		if len(p.From) != 40 || len(p.To) != 40 || p.Amount <= 0 || p.From == p.To {
			return 0
		}
		// reject if already locked
		if _, exists := loadEscrow(p.ID); exists {
			return 0
		}
		fromBal, ok := getInt(balKey(p.From))
		if !ok || fromBal < p.Amount {
			return 0
		}
		rec, err := json.Marshal(escrowRec{From: p.From, To: p.To, Amount: p.Amount})
		if err != nil {
			return 0
		}
		if !putInt(balKey(p.From), fromBal-p.Amount) {
			return 0
		}
		if !sdk.PutState(escrowKey(p.ID), rec) {
			return 0
		}

	case "release":
		e, exists := loadEscrow(p.ID)
		if !exists {
			return 0
		}
		// only original sender (or either party — here: sender or beneficiary) may release
		if p.From != e.From && p.From != e.To {
			return 0
		}
		toBal, ok := getInt(balKey(e.To))
		if !ok {
			return 0
		}
		if !putInt(balKey(e.To), toBal+e.Amount) {
			return 0
		}
		// clear escrow
		if !sdk.PutState(escrowKey(p.ID), []byte{}) {
			return 0
		}

	case "refund":
		e, exists := loadEscrow(p.ID)
		if !exists {
			return 0
		}
		// only original sender can refund
		if p.From != e.From {
			return 0
		}
		fromBal, ok := getInt(balKey(e.From))
		if !ok {
			return 0
		}
		if !putInt(balKey(e.From), fromBal+e.Amount) {
			return 0
		}
		if !sdk.PutState(escrowKey(p.ID), []byte{}) {
			return 0
		}

	default:
		return 0
	}

	_ = sdk.PutState(receiptKey(p.ID), raw)
	return 1
}

func main() {}
