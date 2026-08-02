package storage

import (
	"encoding/hex"
	"path/filepath"
	"testing"

	"commiting-peer/internal/types"
)

func openTestWS(t *testing.T) *WorldState {
	t.Helper()
	ws, err := NewWorldState(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatalf("NewWorldState: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func TestMVCC_SecondTxConflictInSameBlock(t *testing.T) {
	ws := openTestWS(t)
	key := "balance:alice"
	if err := ws.PutKV(key, []byte("100")); err != nil {
		t.Fatal(err)
	}
	ver, err := ws.GetVersion(key)
	if err != nil {
		t.Fatal(err)
	}
	if ver == "" {
		t.Fatal("expected admin version after PutKV")
	}

	hex50 := hex.EncodeToString([]byte("50"))
	hex40 := hex.EncodeToString([]byte("40"))

	block := types.Block{
		Transactions: []types.Transaction{
			{
				Txid: "tx1",
				RWSet: &types.RWSet{
					Reads:  []types.KVRead{{Key: key, Version: ver}},
					Writes: []types.KVWrite{{Key: key, Value: hex50}},
				},
			},
			{
				Txid: "tx2",
				RWSet: &types.RWSet{
					Reads:  []types.KVRead{{Key: key, Version: ver}}, // same stale version
					Writes: []types.KVWrite{{Key: key, Value: hex40}},
				},
			},
		},
	}

	results, err := ws.ApplyBlock(block, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results len=%d", len(results))
	}
	if results[0].Code != TxValid {
		t.Fatalf("tx1: want VALID got %s (%s)", results[0].Code, results[0].Reason)
	}
	if results[1].Code != TxInvalidMVCC {
		t.Fatalf("tx2: want INVALID_MVCC got %s", results[1].Code)
	}

	val, err := ws.GetKV(key)
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "50" {
		t.Fatalf("balance=%q want 50 (tx1 only)", val)
	}
	newVer, _ := ws.GetVersion(key)
	if newVer != "1:0" {
		t.Fatalf("version=%q want 1:0", newVer)
	}
}

func TestMVCC_BothValidDifferentKeys(t *testing.T) {
	ws := openTestWS(t)
	_ = ws.PutKV("balance:a", []byte("10"))
	_ = ws.PutKV("balance:b", []byte("20"))
	va, _ := ws.GetVersion("balance:a")
	vb, _ := ws.GetVersion("balance:b")

	block := types.Block{
		Transactions: []types.Transaction{
			{
				Txid: "txa",
				RWSet: &types.RWSet{
					Reads:  []types.KVRead{{Key: "balance:a", Version: va}},
					Writes: []types.KVWrite{{Key: "balance:a", Value: hex.EncodeToString([]byte("9"))}},
				},
			},
			{
				Txid: "txb",
				RWSet: &types.RWSet{
					Reads:  []types.KVRead{{Key: "balance:b", Version: vb}},
					Writes: []types.KVWrite{{Key: "balance:b", Value: hex.EncodeToString([]byte("19"))}},
				},
			},
		},
	}
	results, err := ws.ApplyBlock(block, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Code != TxValid {
			t.Fatalf("%s: %s %s", r.Txid, r.Code, r.Reason)
		}
	}
}

func TestGetKVWithVersion_Missing(t *testing.T) {
	ws := openTestWS(t)
	_, ver, found, err := ws.GetKVWithVersion("nope")
	if err != nil || found || ver != "" {
		t.Fatalf("found=%v ver=%q err=%v", found, ver, err)
	}
}

func TestWalletStateVersionAfterMintPath(t *testing.T) {
	ws := openTestWS(t)
	if err := ws.PutBalance("abcdef0123456789abcdef0123456789abcdef01", 100); err != nil {
		t.Fatal(err)
	}
	val, ver, found, err := ws.GetKVWithVersion("balance:abcdef0123456789abcdef0123456789abcdef01")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if string(val) != "100" || ver == "" {
		t.Fatalf("val=%q ver=%q", val, ver)
	}
}
