package launchclient

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
	"licentivo-launch/internal/app"
	"licentivo-launch/internal/httpapi"
	"licentivo-launch/internal/storage"
)

// Exercise the account service, HTTP authentication and SDK against a fresh
// MySQL database. Only the licensing runtime is a double; it never grants access.
func TestAccountStoreOwnershipHTTP(t *testing.T) {
	dsn := os.Getenv("LAUNCH_TEST_ACCOUNT_STORE_DATABASE_DSN")
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

	var unavailable atomic.Bool
	var logins, identities atomic.Int32
	wire := transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/auth/logout" && unavailable.Load() {
			return nil, errors.New("test-only account logout outage")
		}
		if r.URL.Path == "/api/v1/auth/login" {
			logins.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/identity") {
			identities.Add(1)
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	newStore := func(t *testing.T) SessionStore {
		t.Helper()
		store, err := NewSystemSessionStore(t.TempDir())
		if errors.Is(err, ErrSecureStoreUnavailable) {
			return &MemoryStore{}
		}
		if err != nil {
			t.Fatal(err)
		}
		return store
	}
	var runtimeStarts atomic.Int32
	open := func(t *testing.T, store SessionStore, cache string) *Application {
		t.Helper()
		a, err := NewApplication(Config{Origin: server.URL, ProductID: testProduct, Environment: "sandbox", AllowLocalHTTP: true, Store: store, CacheDir: cache, RuntimeFactory: func(c RuntimeConfig) (Runtime, error) {
			runtimeStarts.Add(1)
			return &fakeRuntime{path: c.CachePath}, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		a.client.http.Transport = wire
		t.Cleanup(a.Close)
		return a
	}
	login := func(t *testing.T, a *Application) {
		t.Helper()
		if _, err := a.Login(ctx, "logout-audit@example.invalid", "temporary-logout-test-password"); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("atomic_store_save_failure_does_not_restore_account", func(t *testing.T) {
		store := &accountHTTPFaultStore{SessionStore: newStore(t)}
		cache := t.TempDir()
		first := open(t, store, cache)
		login(t, first)
		token := first.record.Session.AccessToken
		store.fail.Store(true)
		unavailable.Store(true)
		err := first.Logout(ctx)
		if !errors.Is(err, ErrAccountLogoutPending) || !errors.Is(err, ErrAccountLogoutNotPersisted) {
			t.Fatal("missing non-durable logout status", err)
		}
		if _, err = store.Load(ctx, first.key); !errors.Is(err, ErrNoSession) {
			t.Fatal("old account record remained restorable", err)
		}
		if first.runtime != nil {
			t.Fatal("local runtime retained")
		}
		if _, err = service.Authenticate(ctx, token, "desktop"); err != nil {
			t.Fatal("fault did not leave real account unrevoked", err)
		}
		store.fail.Store(false)
		unavailable.Store(false)
		restarted := open(t, store, cache)
		beforeLogin, beforeIdentity := logins.Load(), identities.Load()
		if _, err = restarted.Restore(ctx); !errors.Is(err, ErrNoSession) {
			t.Fatal("failed-save logout resumed old account", err)
		}
		if logins.Load() != beforeLogin || identities.Load() != beforeIdentity {
			t.Fatal("restoration fetched account identity")
		}
		if err = first.Logout(ctx); err != nil {
			t.Fatal("in-memory logout retry did not finish", err)
		}
		if _, err = service.Authenticate(ctx, token, "desktop"); !errors.Is(err, app.ErrUnauthorized) {
			t.Fatal("original session not revoked after retry", err)
		}
	})
	for _, offline := range []bool{false, true} {
		name := "old_instance_401_preserves_new_login"
		if offline {
			name = "old_instance_network_failure_retries_own_queue"
		}
		t.Run(name, func(t *testing.T) {
			unavailable.Store(false)
			store := newStore(t)
			cache := t.TempDir()
			older := open(t, store, cache)
			login(t, older)
			oldToken := older.record.Session.AccessToken
			newer := open(t, store, cache)
			login(t, newer)
			newToken := newer.record.Session.AccessToken
			if oldToken == newToken {
				t.Fatal("server reused login token")
			}
			if _, err := service.Authenticate(ctx, oldToken, "desktop"); !errors.Is(err, app.ErrUnauthorized) {
				t.Fatal("old session was not revoked", err)
			}
			unavailable.Store(offline)
			err := older.Logout(ctx)
			if offline && !errors.Is(err, ErrAccountLogoutPending) || !offline && err != nil {
				t.Fatal("unexpected stale-instance logout", err)
			}
			if !offline {
				if err = older.Logout(ctx); err != nil {
					t.Fatal("completed old logout retry failed", err)
				}
			}
			raw, err := store.Load(ctx, newer.key)
			if err != nil {
				t.Fatal("new account was deleted", err)
			}
			var active rememberedSession
			if json.Unmarshal(raw, &active) != nil || active.Session.AccessToken != newToken || active.LogoutPending {
				t.Fatal("old session overwrote new login")
			}
			clear(raw)
			if _, err = service.Authenticate(ctx, newToken, "desktop"); err != nil {
				t.Fatal("new server session lost", err)
			}
			unavailable.Store(false)
			restarted := open(t, store, cache)
			before := runtimeStarts.Load()
			if _, err = restarted.Restore(ctx); err != nil {
				t.Fatal("new account did not restore", err)
			}
			if restarted.record.Session.AccessToken != newToken || runtimeStarts.Load() != before+1 {
				t.Fatal("old task reopened a licensing cache or restored wrong account")
			}
			tasks, err := restarted.pendingAccounts(ctx)
			if err != nil || len(tasks) != 0 {
				t.Fatal("old account queue did not finish", err)
			}
		})
	}
}

type accountHTTPFaultStore struct {
	SessionStore
	fail atomic.Bool
}

func (s *accountHTTPFaultStore) Save(ctx context.Context, key string, value []byte) error {
	if s.fail.Load() {
		return errors.New("injected atomic secure-store write failure")
	}
	return s.SessionStore.Save(ctx, key, value)
}
