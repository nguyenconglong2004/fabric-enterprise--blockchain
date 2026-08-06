// loyalty_xfer — transfer with tiered fee + loyalty points + auto-redeem bonus.
//
// Flow:
//  1. Debit `amount` from sender.
//  2. Protocol fee by tier (taken from amount):
//       amount < 50  → 0%;  50..99 → 2%;  100..499 → 5%;  >=500 → 8%
//     Recipient gets (amount - fee); fee credits balance:treasury.
//  3. Loyalty points on sender: +amount/10 (integer).
//  4. While points >= 100: burn 100 points, mint +5 bonus to sender.
//
// KV: balance:<addr>, loyalty:<addr>, balance:treasury, loyalty_receipt:<to>.
package main

import (
	"encoding/json"
	"strconv"

	"fabricwasm/sdk"
)

const treasuryAddr = "treasury" // logical key suffix → balance:treasury

type Payload struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Amount int64  `json:"amount"`
	Memo   string `json:"memo" schema:"optional"`
}

func balKey(addr string) []byte     { return []byte("balance:" + addr) }
func loyaltyKey(addr string) []byte { return []byte("loyalty:" + addr) }

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

// feeBPS returns fee in basis points (1% = 100).
func feeBPS(amount int64) int64 {
	switch {
	case amount >= 500:
		return 800 // 8%
	case amount >= 100:
		return 500 // 5%
	case amount >= 50:
		return 200 // 2%
	default:
		return 0
	}
}

func calcFee(amount int64) int64 {
	bps := feeBPS(amount)
	if bps == 0 {
		return 0
	}
	fee := amount * bps / 10000
	if fee < 1 {
		fee = 1
	}
	if fee >= amount {
		fee = amount - 1 // recipient always gets at least 1
	}
	return fee
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
	// need room for fee split
	if p.Amount < 1 {
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
	if len(p.From) != 40 || len(p.To) != 40 || p.Amount <= 0 || p.From == p.To {
		return 0
	}

	fee := calcFee(p.Amount)
	creditTo := p.Amount - fee

	fromBal, ok := getInt(balKey(p.From))
	if !ok || fromBal < p.Amount {
		return 0
	}
	toBal, ok := getInt(balKey(p.To))
	if !ok {
		return 0
	}
	treasBal, ok := getInt(balKey(treasuryAddr))
	if !ok {
		return 0
	}

	if !putInt(balKey(p.From), fromBal-p.Amount) {
		return 0
	}
	if !putInt(balKey(p.To), toBal+creditTo) {
		return 0
	}
	if fee > 0 {
		if !putInt(balKey(treasuryAddr), treasBal+fee) {
			return 0
		}
	}

	// Loyalty: +floor(amount/10) points
	pts, ok := getInt(loyaltyKey(p.From))
	if !ok {
		return 0
	}
	pts += p.Amount / 10

	bonus := int64(0)
	// Auto-redeem: every 100 points → +5 balance (may redeem multiple times)
	for pts >= 100 {
		pts -= 100
		bonus += 5
	}
	if !putInt(loyaltyKey(p.From), pts) {
		return 0
	}
	if bonus > 0 {
		fb, ok := getInt(balKey(p.From))
		if !ok {
			return 0
		}
		if !putInt(balKey(p.From), fb+bonus) {
			return 0
		}
	}

	_ = sdk.PutState([]byte("loyalty_receipt:"+p.To), raw)
	return 1
}

func main() {}
