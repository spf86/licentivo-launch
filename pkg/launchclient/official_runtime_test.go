package launchclient_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"licentivo-launch/pkg/launchclient"
	licentivo "licentivo-launch/pkg/launchclient/internal/licentivo"
)

// This is a local protocol test: no payment provider or production licensing
// service is contacted. The real SDK verifies real Ed25519 signatures.
func TestOfficialSDKRestoresSignedOfflineGrantAndRejectsTampering(t *testing.T) {
	const launchProduct = "11111111-1111-4111-8111-111111111111"
	const runtimeProduct = "33333333-3333-4333-8333-333333333333"
	const keyID = "66666666-6666-4666-8666-666666666666"
	device, err := licentivo.MachineDeviceID(runtimeProduct)
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	now := time.Now().Unix()
	claims := licentivo.Claims{Version: 1, KeyID: keyID, TenantID: "22222222-2222-4222-8222-222222222222", ProductID: runtimeProduct, LicenseID: "44444444-4444-4444-8444-444444444444", DeviceID: device, ActivationID: "55555555-5555-4555-8555-555555555555", Mode: "timed", Features: []string{"export"}, MaxDevices: 1, IssuedAt: now, ExpiresAt: now + 3600}
	payload, _ := json.Marshal(claims)
	signature := ed25519.Sign(key, append([]byte("LicenovaLease/v1\n"), payload...))
	envelope := map[string]any{"version": 1, "key_id": keyID, "payload": base64.RawURLEncoding.EncodeToString(payload), "signature": base64.RawURLEncoding.EncodeToString(signature)}
	var offline atomic.Bool
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline.Load() {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"error":{"code":"SERVER_UNAVAILABLE"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"device_key": "lv_dev_" + strings.Repeat("d", 43), "activation_id": claims.ActivationID, "lease": envelope, "heartbeat_seconds": 600}})
	}))
	defer runtimeServer.Close()
	merchant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline.Load() {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"error":{"code":"UNAVAILABLE"}}`))
			return
		}
		var data any
		switch r.URL.Path {
		case "/api/v1/auth/login":
			data = launchclient.Session{AccessToken: "private-token", User: launchclient.User{ID: "account-a"}, ExpiresAt: time.Now().Add(time.Hour)}
		case "/api/v1/me":
			data = map[string]any{"user": launchclient.User{ID: "account-a"}}
		case "/api/v1/auth/logout":
			data = map[string]bool{"signed_out": true}
		default:
			data = launchclient.Credential{ProductID: launchProduct, LicenseKey: "lv_lic_" + strings.Repeat("a", 43), Version: 1}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer merchant.Close()
	store := &launchclient.MemoryStore{}
	cacheRoot := t.TempDir()
	var cachePath string
	config := launchclient.Config{Origin: merchant.URL, ProductID: launchProduct, Environment: "sandbox", AllowLocalHTTP: true, Store: store, CacheDir: cacheRoot, RuntimeFactory: func(c launchclient.RuntimeConfig) (launchclient.Runtime, error) {
		cachePath = c.CachePath
		return launchclient.NewRuntime(launchclient.ProductConfig{BaseURL: runtimeServer.URL, ProductID: runtimeProduct, TrustedKeys: map[string]string{keyID: base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey))}, AllowHTTP: true}, c)
	}}
	app, err := launchclient.NewApplication(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.Login(context.Background(), "example@example.invalid", "password"); err != nil {
		t.Fatal(err)
	}
	if err = app.RunFeature("export", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	app.Close()
	// Simulate a fresh application object while the OS-protected session store
	// (represented by memory here) and official SDK cache survive the restart.
	app, err = launchclient.NewApplication(config)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	offline.Store(true)
	state, err := app.Restore(context.Background())
	if err != nil || !state.OfflineFallback {
		t.Fatal("real SDK offline restart failed", err)
	}
	if err = app.RunFeature("export", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	called := false
	if err = app.RunFeature("not-purchased", func() error { called = true; return nil }); err == nil || called {
		t.Fatal("missing feature bypassed SDK")
	}
	app.Close()
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var cached map[string]any
	if err = json.Unmarshal(raw, &cached); err != nil {
		t.Fatal(err)
	}
	cached["lease"].(map[string]any)["signature"] = base64.RawURLEncoding.EncodeToString(make([]byte, 64))
	raw, _ = json.Marshal(cached)
	if err = os.WriteFile(cachePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Restore(context.Background()); !errors.Is(err, launchclient.ErrOfflineUnavailable) {
		t.Fatal("tampered signature authorized offline", err)
	}
}
