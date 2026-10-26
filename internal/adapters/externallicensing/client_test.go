package externallicensing

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"licentivo-launch/internal/ports"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSignedTransportAndScope(t *testing.T) {
	key := bytes.Repeat([]byte{23}, 32)
	nonces := map[string]bool{}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !VerifyRequest(r, raw, key, "key-1", time.Now()) {
			t.Error("signature did not cover actual request bytes/path")
			w.WriteHeader(401)
			return
		}
		if nonces[r.Header.Get("X-Launch-Nonce")] {
			t.Error("network attempt reused nonce")
		}
		nonces[r.Header.Get("X-Launch-Nonce")] = true
		requests++
		if r.URL.Path != "/prefix/launch/v1/capabilities" {
			t.Error(r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": ports.ExternalCapabilities{Protocol: ports.ExternalLicensingVersion, ConnectionRef: "merchant", Environment: "sandbox", PurchaseTypes: []string{"perpetual"}, DeliveryTypes: []string{"instructions"}}})
	}))
	defer server.Close()
	cfg := Config{BaseURL: server.URL + "/prefix", ConnectionRef: "merchant", Environment: "sandbox", KeyID: "key-1", Key: key, AllowLocalHTTP: true}
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := client.Capabilities(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if requests != 2 {
		t.Fatal(requests)
	}
	cfg.Environment = "live"
	if _, err := New(cfg); err == nil {
		t.Fatal("live HTTP accepted")
	}
	cfg.Environment = "sandbox"
	cfg.ConnectionRef = "another-merchant"
	client, err = New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Capabilities(context.Background()); err == nil {
		t.Fatal("cross-connection capability accepted")
	}
}
func TestSignatureRejectsTamperingAndExpiry(t *testing.T) {
	key := bytes.Repeat([]byte{31}, 32)
	at := time.Unix(1792886400, 0)
	r := httptest.NewRequest("POST", "https://receiver.example/launch/v1/events", nil)
	r.Header.Set("X-Launch-Key-Id", "key-1")
	r.Header.Set("X-Launch-Timestamp", "1792886400")
	r.Header.Set("X-Launch-Nonce", "AAAAAAAAAAAAAAAAAAAAAA")
	body := []byte(`{"value":1}`)
	r.Header.Set("X-Launch-Signature", Signature(key, "key-1", r.Header.Get("X-Launch-Timestamp"), r.Header.Get("X-Launch-Nonce"), r.Method, r.URL.RequestURI(), body))
	if !VerifyRequest(r, body, key, "key-1", at) {
		t.Fatal("valid signature rejected")
	}
	if VerifyRequest(r, []byte(`{"value":2}`), key, "key-1", at) || VerifyRequest(r, body, key, "key-1", at.Add(301*time.Second)) {
		t.Fatal("tampered/expired signature accepted")
	}
	r.URL.Path = "/different"
	if VerifyRequest(r, body, key, "key-1", at) {
		t.Fatal("path not signed")
	}
}
func TestRedirectAndPrivateNetworkDenied(t *testing.T) {
	key := bytes.Repeat([]byte{41}, 32)
	leaked := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer source.Close()
	cfg := Config{BaseURL: source.URL, ConnectionRef: "merchant", Environment: "sandbox", KeyID: "key", Key: key, AllowLocalHTTP: true}
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Capabilities(context.Background()); err == nil || leaked {
		t.Fatal("credential redirect was followed")
	}
	cfg.BaseURL = "https://127.0.0.1:1"
	cfg.AllowLocalHTTP = false
	client, err = New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Capabilities(context.Background()); err == nil {
		t.Fatal("private destination accepted")
	}
}
