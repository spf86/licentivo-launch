package launchclient_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"licentivo-launch/pkg/launchclient"
	licentivo "licentivo-launch/pkg/launchclient/internal/licentivo"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOfficialSDKAccountExchangeAndLogout(t *testing.T) {
	const product = "11111111-1111-4111-8111-111111111111"
	const remote = "33333333-3333-4333-8333-333333333333"
	const connection = "22222222-2222-4222-8222-222222222222"
	const activation = "55555555-5555-4555-8555-555555555555"
	const keyID = "66666666-6666-4666-8666-666666666666"
	device, e := licentivo.MachineDeviceID(remote)
	if e != nil {
		t.Fatal(e)
	}
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	at := time.Now().Unix()
	claims := map[string]any{"version": 1, "key_id": keyID, "tenant_id": connection, "product_id": remote, "license_id": "44444444-4444-4444-8444-444444444444", "device_id": device, "activation_id": activation, "mode": "timed", "features": []string{"export"}, "max_devices": 1, "issued_at": at, "expires_at": at + 600, "runtime_policy": map[string]any{"offline_mode": "none", "offline_seconds": 0, "heartbeat_seconds": 600}, "capabilities": map[string]any{"license_model": "node_locked", "launch_connection_id": connection, "environment": "sandbox", "session_id": "77777777-7777-4777-8777-777777777777", "session_expires_at": at + 3600, "binding_version": 1}}
	raw, _ := json.Marshal(claims)
	signature := ed25519.Sign(key, append([]byte("LicenovaLease/v1\n"), raw...))
	env := map[string]any{"version": 1, "key_id": keyID, "payload": base64.RawURLEncoding.EncodeToString(raw), "signature": base64.RawURLEncoding.EncodeToString(signature)}
	released := false
	licensing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		switch r.URL.Path {
		case "/api/v1/launch/exchange":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["license_key"] != "" || body["identity_token"] == "" {
				t.Error("exchange used a long-term key")
			}
			if name, ok := body["device_name"].(string); !ok || strings.TrimSpace(name) == "" {
				t.Error("SDK did not supply a recognizable device name")
			}
			if details, ok := body["device_info"].(map[string]any); !ok || details["system_type"] == "" {
				t.Error("SDK did not report the computer's display details")
			}
			data = map[string]any{"device_key": "lv_dev_" + strings.Repeat("d", 43), "activation_id": activation, "lease": env, "heartbeat_seconds": 600, "logout_ticket": map[string]any{"operation_id": keyID, "product_id": remote, "activation_id": activation, "expected_generation": 1, "release_token": "lv_out_" + strings.Repeat("o", 43)}}
		case "/api/v1/sessions/logout":
			released = true
			data = map[string]any{"operation_id": keyID, "status": "succeeded", "activation_id": activation, "expected_generation": 1}
		default:
			t.Errorf("unexpected SDK request %s", r.URL.Path)
			data = map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer licensing.Close()
	merchant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		switch r.URL.Path {
		case "/api/v1/auth/login":
			data = launchclient.Session{AccessToken: "private-token", User: launchclient.User{ID: "account-a"}, ExpiresAt: time.Now().Add(time.Hour)}
		case "/api/v1/auth/logout":
			data = map[string]bool{"signed_out": true}
		default:
			data = launchclient.Identity{ProductID: product, LicentivoProductID: remote, LaunchConnectionID: connection, Environment: "sandbox", IdentityToken: strings.Repeat("identity-only-", 4), ExpiresAt: time.Now().Add(time.Minute)}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer merchant.Close()
	var path string
	app, e := launchclient.NewApplication(launchclient.Config{Origin: merchant.URL, ProductID: product, Environment: "sandbox", AllowLocalHTTP: true, CacheDir: t.TempDir(), RuntimeFactory: func(c launchclient.RuntimeConfig) (launchclient.Runtime, error) {
		path = c.CachePath
		return launchclient.NewRuntime(launchclient.ProductConfig{BaseURL: licensing.URL, ProductID: remote, TrustedKeys: map[string]string{keyID: base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey))}, AllowHTTP: true}, c)
	}})
	if e != nil {
		t.Fatal(e)
	}
	defer app.Close()
	if _, e = app.Login(context.Background(), "a@example.invalid", "password"); e != nil {
		t.Fatal(e)
	}
	if e = app.RunFeature("export", func() error { return nil }); e != nil {
		t.Fatal(e)
	}
	if e = app.Logout(context.Background()); e != nil {
		t.Fatal(e)
	}
	if !released {
		t.Fatal("official logout not sent")
	}
	if _, e = os.Stat(path + ".logout.json"); e != nil {
		t.Fatal("SDK logout journal was deleted", e)
	}
}
