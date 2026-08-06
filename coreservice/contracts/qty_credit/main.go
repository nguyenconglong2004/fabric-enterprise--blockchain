// qty_credit — sender loses amount; recipient gains amount * quantity.
package main

import (
	"encoding/json"
	"strconv"

	"fabricwasm/sdk"
)

type Payload struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Amount   int64  `json:"amount"`
	Quantity int64  `json:"quantity"`
	Memo     string `json:"memo" schema:"optional"`
}

func balKey(addr string) []byte { return []byte("balance:" + addr) }

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

//export verify_tx
func verify_tx(ptr uint32, size uint32) uint32 {
	if size == 0 || size > 4096 {
		return 0
	}
	var p Payload
	if err := json.Unmarshal(sdk.PayloadSlice(ptr, size), &p); err != nil {
		return 0
	}
	if len(p.From) != 40 || len(p.To) != 40 || p.Amount <= 0 || p.Quantity <= 0 {
		return 0
	}
	if p.From == p.To {
		return 0
	}
	if p.Quantity > 0 && p.Amount > (1<<62)/p.Quantity {
		return 0
	}
	if len(p.Memo) > 200 {
		return 0
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

	credit := p.Amount * p.Quantity
	fromBal, ok := getInt(balKey(p.From))
	if !ok || fromBal < p.Amount {
		return 0
	}
	toBal, ok := getInt(balKey(p.To))
	if !ok {
		return 0
	}
	if !putInt(balKey(p.From), fromBal-p.Amount) {
		return 0
	}
	if !putInt(balKey(p.To), toBal+credit) {
		return 0
	}
	_ = sdk.PutState([]byte("qty_receipt:"+p.To), raw)
	return 1
}

func main() {}
