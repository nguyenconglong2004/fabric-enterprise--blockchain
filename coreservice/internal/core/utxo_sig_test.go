package core

import (
	"testing"

	"coreservice/internal/wallet"
)

func TestVerifyVinScriptSigs_OK(t *testing.T) {
	_, priv, pub, err := wallet.NewKeypair()
	if err != nil {
		t.Fatal(err)
	}
	addr := wallet.AddressFromPub(pub)
	asm, hexScript, addrs := wallet.MakeP2PKHScriptPubKey(addr)
	prev := VOUT{
		Value: 1000,
		N:     0,
		ScriptPubKey: ScriptPubKey{
			ASM: asm, Hex: hexScript, Addresses: addrs,
		},
	}
	tx := &Transaction{
		Version: 1,
		Vin: []VIN{{
			Txid: "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899",
			Vout: 0,
		}},
		Vout: []VOUT{{
			Value:        900,
			N:            0,
			ScriptPubKey: ScriptPubKey{ASM: asm, Hex: hexScript, Addresses: addrs},
		}},
	}
	if err := tx.SignVinEd25519(priv, []VOUT{prev}); err != nil {
		t.Fatal(err)
	}
	if err := tx.VerifyVinScriptSigs([]VOUT{prev}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyVinScriptSigs_BadSig(t *testing.T) {
	_, priv, pub, err := wallet.NewKeypair()
	if err != nil {
		t.Fatal(err)
	}
	addr := wallet.AddressFromPub(pub)
	asm, hexScript, addrs := wallet.MakeP2PKHScriptPubKey(addr)
	prev := VOUT{
		Value: 100, N: 0,
		ScriptPubKey: ScriptPubKey{ASM: asm, Hex: hexScript, Addresses: addrs},
	}
	tx := &Transaction{
		Version: 1,
		Vin:     []VIN{{Txid: "11", Vout: 0}},
		Vout: []VOUT{{
			Value: 50, N: 0,
			ScriptPubKey: ScriptPubKey{ASM: asm, Hex: hexScript, Addresses: addrs},
		}},
	}
	if err := tx.SignVinEd25519(priv, []VOUT{prev}); err != nil {
		t.Fatal(err)
	}
	// Tamper value after signing → sighash mismatch.
	tx.Vout[0].Value = 99
	if err := tx.VerifyVinScriptSigs([]VOUT{prev}); err == nil {
		t.Fatal("expected verify failure after tamper")
	}
}
