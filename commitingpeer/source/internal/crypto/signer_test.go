package crypto

import (
	"strings"
	"testing"

	"github.com/cloudflare/circl/sign/mldsa/mldsa44"
)

func TestEd25519SignerRoundTrip(t *testing.T) {
	s, err := generateEd25519Signer()
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("7b2276223a226b362d312d31227d")
	sig, err := s.SignTx("k6-1-1", "bench_ping", payload)
	if err != nil {
		t.Fatal(err)
	}
	if !s.VerifyTx("k6-1-1", "bench_ping", payload, sig, s.PublicKeyHex()) {
		t.Fatal("self-verify failed")
	}
	if !VerifyEndorsement("k6-1-1", "bench_ping", payload, AlgoEd25519, sig, s.PublicKeyHex()) {
		t.Fatal("VerifyEndorsement failed")
	}
}

func TestMLDSA44SignerRoundTrip(t *testing.T) {
	s, err := generateMLDSA44Signer()
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("7b2276223a226b362d312d31227d")
	sig, err := s.SignTx("k6-1-1", "bench_ping", payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(sig) != mldsa44.SignatureSize*2 {
		t.Fatalf("sig hex len = %d want %d", len(sig), mldsa44.SignatureSize*2)
	}
	if !s.VerifyTx("k6-1-1", "bench_ping", payload, sig, s.PublicKeyHex()) {
		t.Fatal("self-verify failed")
	}
	if !VerifyEndorsement("k6-1-1", "bench_ping", payload, AlgoMLDSA44, sig, s.PublicKeyHex()) {
		t.Fatal("VerifyEndorsement failed")
	}
}

func TestParseTrustedKey(t *testing.T) {
	algo, pub, err := ParseTrustedKey("mldsa-44:abc")
	if err != nil || algo != AlgoMLDSA44 || pub != "abc" {
		t.Fatalf("parsed %#v %q err=%v", algo, pub, err)
	}
	algo, pub, err = ParseTrustedKey("deadbeef")
	if err != nil || algo != AlgoEd25519 || pub != "deadbeef" {
		t.Fatalf("bare hex parsed %#v %q err=%v", algo, pub, err)
	}
}

func TestResolveKeyAlgorithm(t *testing.T) {
	ed, _ := generateEd25519Signer()
	algo, err := ResolveKeyAlgorithm(ed.PrivateKeyHex())
	if err != nil || algo != AlgoEd25519 {
		t.Fatalf("ed25519 resolve: %v %s", err, algo)
	}
	pq, _ := generateMLDSA44Signer()
	algo, err = ResolveKeyAlgorithm(pq.PrivateKeyHex())
	if err != nil || algo != AlgoMLDSA44 {
		t.Fatalf("mldsa resolve: %v %s", err, algo)
	}
}

func TestParseAlgorithm(t *testing.T) {
	for _, raw := range []string{"", "ed25519", "1", "mldsa-44", "2", "MLDSA44"} {
		if _, err := ParseAlgorithm(raw); err != nil {
			t.Fatalf("ParseAlgorithm(%q): %v", raw, err)
		}
	}
	if _, err := ParseAlgorithm("rsa"); err == nil {
		t.Fatal("expected error for rsa")
	}
}

func TestSignTxMessageCompat(t *testing.T) {
	s, _ := generateEd25519Signer()
	priv := s.PrivateKeyHex()
	sig1, err := SignTxMessage("tx", "bench_ping", []byte("aa"), priv)
	if err != nil {
		t.Fatal(err)
	}
	sig2, err := s.SignTx("tx", "bench_ping", []byte("aa"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(sig1, sig2) {
		t.Fatalf("legacy SignTxMessage mismatch")
	}
}
