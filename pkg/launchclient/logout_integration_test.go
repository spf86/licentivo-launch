package launchclient

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-sql-driver/mysql"
	"licentivo-launch/internal/app"
	"licentivo-launch/internal/httpapi"
	"licentivo-launch/internal/storage"
)

// Exercise the account service, HTTP authentication and SDK against a fresh
// MySQL database. Only the licensing runtime is a double; it never grants access.
func TestLaunchAccountLogoutRecovery(t *testing.T) {
	dsn := os.Getenv("LAUNCH_TEST_LOGOUT_DATABASE_DSN")
	if dsn == "" {
		t.Skip("dedicated logout audit database required")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "launch_test_") {
		t.Fatal("dedicated test database required")
	}
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close(db)
	var tables int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()").Scan(&tables).Error; err != nil || tables != 0 {
		t.Fatal("fresh audit database required", err)
	}
	ctx := context.Background()
	if err := storage.Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	var handler http.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer server.Close()
	service, err := app.New(ctx, db, app.Config{PublicURL: server.URL, MasterKey: bytes.Repeat([]byte{36}, 32), SetupToken: strings.Repeat("q", 43), UploadDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Bootstrap(ctx, app.BootstrapInput{Token: strings.Repeat("q", 43), Email: "logout-audit@example.invalid", Password: "temporary-logout-test-password", BrandName: "Logout Audit"}); err != nil {
		t.Fatal(err)
	}
	remote := testRemote
	if err := db.Create(&storage.Product{ID: testProduct, LicentivoProductID: &remote, Slug: "logout-audit", Name: "Logout Audit", Content: []byte(`{}`), Status: "published"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&storage.LaunchTrustConnection{Environment: "sandbox", RemoteID: testConnection, Issuer: server.URL, Audience: app.IdentityAudience}).Error; err != nil {
		t.Fatal(err)
	}
	handler = httpapi.Handler(httpapi.Options{App: service, PublicURL: server.URL})
	var store SessionStore
	protected, storeErr := NewSystemSessionStore(t.TempDir())
	if storeErr == nil {
		store = protected
	} else if errors.Is(storeErr, ErrSecureStoreUnavailable) {
		store = &MemoryStore{}
	} else {
		t.Fatal(storeErr)
	}
	var runtime *fakeRuntime
	account, err := NewApplication(Config{Origin: server.URL, ProductID: testProduct, Environment: "sandbox", AllowLocalHTTP: true, Store: store, CacheDir: t.TempDir(), RuntimeFactory: func(c RuntimeConfig) (Runtime, error) {
		runtime = &fakeRuntime{path: c.CachePath}
		return runtime, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer account.Close()
	var unavailable atomic.Bool
	var acceptedLogouts atomic.Int32
	base := account.client.http.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	account.client.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/auth/logout" {
			if unavailable.Load() {
				return nil, errors.New("test-only logout connection outage")
			}
			acceptedLogouts.Add(1)
		}
		return base.RoundTrip(r)
	})
	if _, err := account.Login(ctx, "logout-audit@example.invalid", "temporary-logout-test-password"); err != nil {
		t.Fatal(err)
	}
	token := account.record.Session.AccessToken
	unavailable.Store(true)
	if err := account.Logout(ctx); !errors.Is(err, ErrAccountLogoutPending) {
		t.Fatal("missing pending account revocation", err)
	}
	if runtime.released != 1 || account.runtime != nil {
		t.Fatal("local runtime was not stopped")
	}
	raw, err := store.Load(ctx, account.key)
	if err != nil || !bytes.Contains(raw, []byte(`"logout_pending":true`)) {
		t.Fatal("durable account logout intent missing", err)
	}
	clear(raw)
	if _, err := service.Authenticate(ctx, token, "desktop"); err != nil {
		t.Fatal("test outage did not preserve the original unrevoked session", err)
	}
	account.Close()
	restarted, err := NewApplication(account.config)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	restarted.client.http.Transport = account.client.http.Transport
	if _, err := restarted.Restore(ctx); !errors.Is(err, ErrAccountLogoutPending) {
		t.Fatal("pending account restored after SDK restart", err)
	}
	if restarted.runtime != nil {
		t.Fatal("pending account started authorization")
	}
	unavailable.Store(false)
	if _, err := restarted.Restore(ctx); !errors.Is(err, ErrNoSession) {
		t.Fatal("completed logout restored a login", err)
	}
	if acceptedLogouts.Load() == 0 {
		t.Fatal("no actual logout request after transport recovery")
	}
	if _, err := store.Load(ctx, restarted.key); !errors.Is(err, ErrNoSession) {
		t.Fatal("confirmed account task retained", err)
	}
	if _, err := service.Authenticate(ctx, token, "desktop"); !errors.Is(err, app.ErrUnauthorized) {
		t.Fatal("old server session survived logout recovery", err)
	}
	_, err = account.client.WithToken(token).Identity(ctx, testProduct, "sandbox")
	var denied *APIError
	if !errors.As(err, &denied) || denied.Status != 401 {
		t.Fatal("old token fetched a fresh identity", err)
	}
}
