package backup

import (
	"bytes"
	"testing"
)

func TestEncryptedArchive(t *testing.T) {
	plain := []byte(`{"master_key":"private test key","amount_minor":9223372036854775807}`)
	password := "test-only-backup-passphrase"
	sealed, err := Seal(plain, password)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("private test key")) {
		t.Fatal("backup key appeared in plaintext")
	}
	restored, err := Open(sealed, password)
	if err != nil || !bytes.Equal(plain, restored) {
		t.Fatal("archive did not round trip exactly")
	}
	if _, err := Open(sealed, "wrong-test-passphrase"); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := Open(sealed, password); err == nil {
		t.Fatal("altered archive accepted")
	}
}
