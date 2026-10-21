package security

import (
	"bytes"
	"errors"
	"testing"
)

func TestVaultAuthenticatesRecordPurposeAndKey(t *testing.T) {
	v, err := NewVault(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("test-only-license-material")
	a, err := v.Seal(secret, "license-key:record-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := v.Seal(secret, "license-key:record-a")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) || bytes.Contains(a, secret) {
		t.Fatal("ciphertext was deterministic or plaintext")
	}
	plain, err := v.Open(a, "license-key:record-a")
	if err != nil || !bytes.Equal(plain, secret) {
		t.Fatal("roundtrip failed")
	}
	if _, err := v.Open(a, "license-key:record-b"); !errors.Is(err, ErrCiphertext) {
		t.Fatal("record substitution accepted")
	}
	other, _ := NewVault(bytes.Repeat([]byte{2}, 32))
	if _, err := other.Open(a, "license-key:record-a"); !errors.Is(err, ErrCiphertext) {
		t.Fatal("wrong key accepted")
	}
	a[len(a)-1] ^= 1
	if _, err := v.Open(a, "license-key:record-a"); !errors.Is(err, ErrCiphertext) {
		t.Fatal("tampering accepted")
	}
	if _, err := v.Open([]byte{1}, "license-key:record-a"); err == nil {
		t.Fatal("truncated envelope accepted")
	}
}
