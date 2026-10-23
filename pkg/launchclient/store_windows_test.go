//go:build windows

package launchclient

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestDPAPIStoreEncryptsAndBindsScope(t *testing.T) {
	store, err := NewSystemSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := store.(*protectedFileStore)
	ctx := context.Background()
	secret := []byte(`{"access_token":"private-bearer","license_key":"private-license"}`)
	if err := store.Save(ctx, "account-product-sandbox", secret); err != nil {
		t.Fatal(err)
	}
	cipher, err := os.ReadFile(s.path("account-product-sandbox"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cipher, []byte("private-bearer")) || bytes.Contains(cipher, []byte("private-license")) {
		t.Fatal("session secret stored plaintext")
	}
	loaded, err := store.Load(ctx, "account-product-sandbox")
	if err != nil || !bytes.Equal(loaded, secret) {
		t.Fatal("DPAPI roundtrip failed", err)
	}
	if err = os.WriteFile(s.path("account-product-live"), cipher, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Load(ctx, "account-product-live"); err == nil {
		t.Fatal("encrypted session crossed environment scope")
	}
	if err = store.Save(ctx, "account-product-sandbox", []byte("updated-secret")); err != nil {
		t.Fatal("atomic replacement failed", err)
	}
	loaded, err = store.Load(ctx, "account-product-sandbox")
	if err != nil || string(loaded) != "updated-secret" {
		t.Fatal(err)
	}
}
