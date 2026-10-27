package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"licentivo-launch/internal/adapters/licentivo"
	"licentivo-launch/internal/app"
	"licentivo-launch/internal/httpapi"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

type httpSetupCatalog struct {
	productsErr, policiesErr error
}

func (c httpSetupCatalog) Products(context.Context) (json.RawMessage, error) {
	if c.productsErr != nil {
		return nil, c.productsErr
	}
	return json.RawMessage(`{"items":[]}`), nil
}
func (c httpSetupCatalog) Policies(context.Context) (json.RawMessage, error) {
	if c.policiesErr != nil {
		return nil, c.policiesErr
	}
	return json.RawMessage(`{"items":[]}`), nil
}
func (httpSetupCatalog) Capabilities(context.Context) (ports.LicensingCapabilities, error) {
	return ports.LicensingCapabilities{ManagementAPI: true, DeviceOperations: true, PreserveUsage: true, IdempotentRecovery: true, CompareAndSwap: true}, nil
}

func TestSDKHTTPSettingsAndSavedConnectionDiagnostics(t *testing.T) {
	dsn := os.Getenv("LAUNCH_TEST_HTTP_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set LAUNCH_TEST_HTTP_DATABASE_DSN to a fresh launch_test_* database")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(parsed.DBName, "launch_test_") {
		t.Fatal("isolated launch_test_* database required")
	}
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close(db)
	var tables int64
	if err = db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()").Scan(&tables).Error; err != nil || tables != 0 {
		t.Fatal("fresh HTTP test database required", err)
	}
	if err = storage.Initialize(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var accept atomic.Bool
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": []any{}}})
			return
		}
		if !accept.Load() {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "LAUNCH_HTTPS_REQUIRED", "message": "HTTP origin denied"}, "request_id": "request-http-integration"})
			return
		}
		var in ports.LaunchConnectionInput
		json.NewDecoder(r.Body).Decode(&in)
		id, _ := security.ID()
		json.NewEncoder(w).Encode(map[string]any{"data": ports.LaunchConnection{ID: id, Issuer: in.Issuer, Audience: in.Audience, Environment: in.Environment, Enabled: true}})
	}))
	defer remote.Close()
	key := bytes.Repeat([]byte{4}, 32)
	var rejectSavedKey bool
	config := app.Config{PublicURL: "http://customer-test-server:18080", MasterKey: key, SetupToken: strings.Repeat("s", 43), UploadDir: t.TempDir(), LicensingSetupFactory: func(apiKey string) (app.LicentivoSetupProvider, error) {
		denied := &licentivo.APIError{Status: 401, Code: "UNAUTHENTICATED"}
		if rejectSavedKey || apiKey == "lv_api_"+strings.Repeat("b", 43) {
			return httpSetupCatalog{productsErr: denied}, nil
		}
		if apiKey == "lv_api_"+strings.Repeat("c", 43) {
			return httpSetupCatalog{policiesErr: denied}, nil
		}
		return httpSetupCatalog{}, nil
	}, BridgeFactory: func(key string) (ports.LaunchBridge, error) { return licentivo.NewBridgeClient(remote.URL, key, true) }}
	svc, err := app.New(context.Background(), db, config)
	if err != nil {
		t.Fatal(err)
	}
	a := &testApp{t, httpapi.Handler(httpapi.Options{App: svc, PublicURL: config.PublicURL, PublicURLProvider: svc.PublicURL}), db, key}
	website := func(r *http.Request) { r.Header.Set("Origin", svc.PublicURL()) }
	desktop := func(r *http.Request) { r.Header.Del("Origin") }
	admin := &browser{}
	a.expect(a.request("POST", "/api/v1/setup", map[string]string{"setup_token": config.SetupToken, "email": "http-test@example.invalid", "password": "test-owner-password-123", "brand_name": "HTTP Tests"}, nil, website), 200)
	a.expect(a.request("POST", "/api/v1/auth/login", map[string]string{"email": "http-test@example.invalid", "password": "test-owner-password-123"}, admin, website), 200)
	settings := data[app.TransportSettingsView](t, a.request("GET", "/api/v1/admin/transport/settings", nil, admin))
	if settings.AllowSDKHTTP {
		t.Fatal("SDK HTTP must default off")
	}
	login := map[string]string{"email": "http-test@example.invalid", "password": "test-owner-password-123", "kind": "desktop"}
	denied := a.request("POST", "/api/v1/auth/login", login, nil, desktop)
	a.expect(denied, 403)
	if !strings.Contains(denied.Body.String(), "SDK_HTTP_DISABLED") {
		t.Fatal("missing HTTP switch explanation")
	}
	update := map[string]any{"mode": "http", "public_url": config.PublicURL, "allow_sdk_http": true}
	a.expect(a.request("POST", "/api/v1/admin/transport/settings", update, admin, website), 200)
	grant := data[map[string]any](t, a.request("POST", "/api/v1/auth/login", login, nil, desktop))
	token, _ := grant["access_token"].(string)
	if token == "" {
		t.Fatal("enabled HTTP desktop login failed")
	}
	bearer := func(r *http.Request) { r.Header.Del("Origin"); r.Header.Set("Authorization", "Bearer "+token) }
	// A temporary authentication storage outage must not claim that this
	// still-valid session is revoked, especially while the SDK retries logout.
	const failSessionLookup = "test:sdk_session_storage_outage"
	if err := db.Callback().Query().Before("gorm:query").Register(failSessionLookup, func(tx *gorm.DB) {
		if tx.Statement.Table == "sessions" {
			tx.AddError(errors.New("test database connection lost"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Query().Remove(failSessionLookup) })
	a.expect(a.request("GET", "/api/v1/me", nil, nil, bearer), 503)
	a.expect(a.request("POST", "/api/v1/auth/logout", map[string]any{}, nil, bearer), 503)
	if err := db.Callback().Query().Remove(failSessionLookup); err != nil {
		t.Fatal(err)
	}
	a.expect(a.request("GET", "/api/v1/me", nil, nil, bearer), 200)
	live := a.request("POST", "/api/v1/software/products/11111111-1111-4111-8111-111111111111/identity", map[string]string{"environment": "live"}, nil, bearer)
	a.expect(live, 403)
	if !strings.Contains(live.Body.String(), "SDK_HTTPS_REQUIRED") {
		t.Fatal("live HTTP was not rejected")
	}
	status := a.request("GET", "/api/v1/software/products/11111111-1111-4111-8111-111111111111/status?environment=live", nil, nil, bearer)
	a.expect(status, 403)
	if !strings.Contains(status.Body.String(), "SDK_HTTPS_REQUIRED") {
		t.Fatal("live HTTP status access was not rejected")
	}
	update["allow_sdk_http"] = false
	a.expect(a.request("POST", "/api/v1/admin/transport/settings", update, admin, website), 200)
	a.expect(a.request("GET", "/api/v1/me", nil, nil, bearer), 403)
	// Turning off HTTP must still allow an existing desktop session to revoke
	// itself, so SDK logout retries can finish without re-enabling HTTP access.
	a.expect(a.request("POST", "/api/v1/auth/logout", map[string]any{}, nil, bearer), 200)
	var signedOut storage.Session
	if err := db.First(&signedOut, "token_hash = ?", security.Digest(token)).Error; err != nil || signedOut.RevokedAt == nil {
		t.Fatal("disabling HTTP left the desktop session active after logout", err)
	}
	a.expect(a.request("POST", "/api/v1/auth/logout", map[string]any{}, nil, bearer), 401)
	if restarted, err := app.New(context.Background(), db, config); err != nil {
		t.Fatal(err)
	} else if allowed, err := restarted.SDKHTTPEnabled(context.Background()); err != nil || allowed {
		t.Fatal("switch state did not persist", err)
	}

	failure := a.request("POST", "/api/v1/admin/connections/licentivo", map[string]string{"api_key": "lv_api_" + strings.Repeat("a", 43)}, admin, website)
	a.expect(failure, 400)
	if !strings.Contains(failure.Body.String(), "LAUNCH_HTTPS_REQUIRED") || !strings.Contains(failure.Body.String(), "request-http-integration") {
		t.Fatal("specific upstream failure was lost")
	}
	connections := data[map[string]any](t, a.request("GET", "/api/v1/admin/connections", nil, admin))
	row := connections["licensing"].([]any)[0].(map[string]any)
	if row["account_protocol_ready"] != false || !strings.Contains(row["last_check_error"].(string), "接入设置") {
		t.Fatal("saved status implied successful authorization")
	}
	accept.Store(true)
	a.expect(a.request("POST", "/api/v1/admin/connections/licentivo", map[string]string{}, admin, website), 200)
	connections = data[map[string]any](t, a.request("GET", "/api/v1/admin/connections", nil, admin))
	row = connections["licensing"].([]any)[0].(map[string]any)
	if row["account_protocol_ready"] != true || row["last_check_error"] != nil {
		t.Fatal("successful reconnect did not clear failure")
	}
	var saved storage.LicentivoConnection
	if err := db.First(&saved).Error; err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []string{strings.Repeat("b", 43), strings.Repeat("c", 43)} {
		failure := a.request("POST", "/api/v1/admin/connections/licentivo", map[string]string{"api_key": "lv_api_" + replacement}, admin, website)
		a.expect(failure, 400)
		if !strings.Contains(failure.Body.String(), "LICENSE_KEY_INVALID") {
			t.Fatal("replacement-key error was lost")
		}
		connections = data[map[string]any](t, a.request("GET", "/api/v1/admin/connections", nil, admin))
		row = connections["licensing"].([]any)[0].(map[string]any)
		if row["account_protocol_ready"] != true || row["last_check_error"] != nil {
			t.Fatal("a rejected replacement key invalidated the unchanged working connection")
		}
		var unchanged storage.LicentivoConnection
		if err := db.First(&unchanged).Error; err != nil {
			t.Fatal(err)
		}
		if unchanged.CredentialSecretID != saved.CredentialSecretID || string(unchanged.Catalog) != string(saved.Catalog) || !unchanged.LastCheckedAt.Equal(*saved.LastCheckedAt) {
			t.Fatal("a rejected replacement key changed saved credentials or catalog diagnostics")
		}
	}
	rejectSavedKey = true
	a.expect(a.request("POST", "/api/v1/admin/connections/licentivo", map[string]string{}, admin, website), 400)
	connections = data[map[string]any](t, a.request("GET", "/api/v1/admin/connections", nil, admin))
	row = connections["licensing"].([]any)[0].(map[string]any)
	if row["account_protocol_ready"] != false || row["last_check_error"] == nil {
		t.Fatal("failure of the saved key did not invalidate its connection diagnostics")
	}
	if err := db.Model(&storage.InstanceSettings{}).Where("id = ?", 1).Update("public_origin", "http://CUSTOMER-test-server:18080/").Error; err != nil {
		t.Fatal(err)
	}
	if restarted, err := app.New(context.Background(), db, config); err != nil {
		t.Fatal(err)
	} else if restarted.PublicURL() != config.PublicURL {
		t.Fatal("a legacy saved public origin was not normalized on restart", restarted.PublicURL())
	}
}
