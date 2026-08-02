// transfer — account-model contract: execute moves balance:<from> → balance:<to>.
// Discount (KV discount:<from>): debit = ceil(amount / (1+d)); credit = amount.
package main

import (
	"encoding/json"
	"math"
	"strconv"

	"fabricwasm/sdk"
)

type Payload struct {
	From   string  `json:"from"`
	To     string  `json:"to"`
	Amount int64   `json:"amount"`
	Memo   string  `json:"memo"`
}

func balKey(addr string) []byte { return []byte("balance:" + addr) }
func discKey(addr string) []byte { return []byte("discount:" + addr) }

func getInt(key []byte) (int64, bool) {
	n := sdk.SizeOf(key)
	if n == 0 {
		return 0, true // missing → 0
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

func getDiscount(addr string) float64 {
	key := discKey(addr)
	n := sdk.SizeOf(key)
	if n == 0 {
		return 0
	}
	buf := make([]byte, n)
	got, ok := sdk.GetState(key, buf)
	if !ok || got == 0 {
		return 0
	}
	d, err := strconv.ParseFloat(string(buf[:got]), 64)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

func putInt(key []byte, v int64) bool {
	return sdk.PutState(key, []byte(strconv.FormatInt(v, 10)))
}

//export allocate
func allocate(size uint32) *byte {
	return sdk.Allocate(size)
}

//export verify_tx
func verify_tx(ptr uint32, size uint32) uint32 {
	if size == 0 || size > 4096 {
		return 0
	}
	var p Payload
	if err := json.Unmarshal(sdk.PayloadSlice(ptr, size), &p); err != nil {
		return 0
	}
	if len(p.From) != 40 || len(p.To) != 40 || p.Amount <= 0 {
		return 0
	}
	if p.From == p.To {
		return 0
	}
	if len(p.Memo) > 200 {
		return 0
	}
	return 1
}

//export execute
func execute(ptr uint32, size uint32) uint32 {
	payload := sdk.PayloadSlice(ptr, size)
	var p Payload
	if err := json.Unmarshal(payload, &p); err != nil {
		return 0
	}
	if len(p.From) != 40 || len(p.To) != 40 || p.Amount <= 0 {
		return 0
	}

	d := getDiscount(p.From)
	debit := p.Amount
	if d > 0 {
		debit = int64(math.Ceil(float64(p.Amount)/(1+d) - 1e-12))
		if debit < 1 {
			debit = 1
		}
	}

	fromBal, ok := getInt(balKey(p.From))
	if !ok || fromBal < debit {
		return 0
	}
	toBal, ok := getInt(balKey(p.To))
	if !ok {
		return 0
	}

	if !putInt(balKey(p.From), fromBal-debit) {
		return 0
	}
	if !putInt(balKey(p.To), toBal+p.Amount) {
		return 0
	}
	// Receipt for explorer / audit
	_ = sdk.PutState([]byte("xfer_receipt:"+p.To), payload)
	return 1
}

func main() {}
