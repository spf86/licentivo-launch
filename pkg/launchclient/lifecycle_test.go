package launchclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testProduct = "11111111-1111-4111-8111-111111111111"

type fakeRuntime struct {
	offline                           bool
	denied                            bool
	started, closed, released, checks int
}

func (r *fakeRuntime) Start(context.Context, func(error)) error { r.started++; return nil }
func (r *fakeRuntime) CanStartOffline() bool                    { return r.offline }
func (r *fakeRuntime) RunFeature(_ string, action func() error) error {
	r.checks++
	if r.denied {
		return errors.New("SDK denial")
	}
	return action()
}
func (r *fakeRuntime) Deactivate(context.Context) error { r.released++; return nil }
func (r *fakeRuntime) Close()                           { r.closed++ }

type lifecycleFixture struct {
	app                  *Application
	store                *MemoryStore
	mode                 string
	user                 string
	offline              bool
	logouts, credentials int
	runtimes             []*fakeRuntime
	configs              []RuntimeConfig
}

func newLifecycleFixture(t *testing.T) *lifecycleFixture {
	t.Helper()
	f := &lifecycleFixture{store: &MemoryStore{}, user: "account-a", offline: true}
	app, err := NewApplication(Config{Origin: "https://merchant.example", ProductID: testProduct, Environment: "sandbox", Store: f.store, CacheDir: t.TempDir(), RetryPolicy: RetryPolicy{MaxAttempts: 3, InitialDelay: time.Nanosecond, MaxDelay: time.Millisecond}, RuntimeFactory: func(config RuntimeConfig) (Runtime, error) {
		runtime := &fakeRuntime{offline: f.offline}
		f.runtimes = append(f.runtimes, runtime)
		f.configs = append(f.configs, config)
		if err := os.WriteFile(config.CachePath, []byte("SDK-owned signed grant"), 0600); err != nil {
			return nil, err
		}
		return runtime, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	f.app = app
	app.client.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if f.mode == "offline" {
			return nil, errors.New("network unavailable")
		}
		status := 200
		var data any
		switch r.URL.Path {
		case "/api/v1/auth/login":
			data = Session{AccessToken: "private-token", User: User{ID: f.user}, ExpiresAt: time.Now().Add(time.Hour)}
		case "/api/v1/auth/logout":
			f.logouts++
			data = map[string]bool{"signed_out": true}
		case "/api/v1/me":
			data = map[string]any{"user": User{ID: f.user}}
		default:
			f.credentials++
			data = Credential{ProductID: testProduct, LicenseKey: "private-license", Version: 1}
		}
		body, _ := json.Marshal(map[string]any{"data": data})
		if f.mode != "" && r.URL.Path != "/api/v1/auth/logout" {
			if f.mode == "unauthorized" {
				status = 401
				body = []byte(`{"error":{"code":"UNAUTHENTICATED"}}`)
			} else if strings.Contains(r.URL.Path, "/credential") {
				status = 403
				body, _ = json.Marshal(map[string]any{"error": map[string]string{"code": f.mode}})
			}
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
	})
	t.Cleanup(app.Close)
	return f
}

func (f *lifecycleFixture) login(t *testing.T) {
	t.Helper()
	if _, err := f.app.Login(context.Background(), "user@example.invalid", "password"); err != nil {
		t.Fatal(err)
	}
}

func TestCloseRestoreAndOfflineRemainSDKAuthoritative(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	f.app.Close()
	if f.logouts != 0 || f.runtimes[0].released != 0 {
		t.Fatal("Close signed out or released a node-locked device")
	}
	if _, err := f.store.Load(context.Background(), f.app.key); err != nil {
		t.Fatal("Close forgot the session", err)
	}
	f.mode = "offline"
	state, err := f.app.Restore(context.Background())
	if err != nil || !state.OfflineFallback {
		t.Fatal("signed SDK fallback unavailable", err)
	}
	last := f.runtimes[len(f.runtimes)-1]
	last.denied = true
	called := false
	if err = f.app.RunFeature("export", func() error { called = true; return nil }); err == nil || called || last.checks != 1 {
		t.Fatal("merchant metadata bypassed SDK feature denial")
	}
	if f.configs[0].CachePath != f.configs[1].CachePath {
		t.Fatal("restore lost account scope")
	}
	f.app.Close()
	f.offline = false
	if _, err = f.app.Restore(context.Background()); !errors.Is(err, ErrOfflineUnavailable) {
		t.Fatal("unsigned/missing grant accepted", err)
	}
	if f.runtimes[len(f.runtimes)-1].started != 0 {
		t.Fatal("offline startup attempted without verified grant")
	}
}

func TestMerchantDenialsNeverFallBack(t *testing.T) {
	for _, code := range []string{"unauthorized", "ENTITLEMENT_EXPIRED", "ENTITLEMENT_SUSPENDED", "ENTITLEMENT_NOT_PURCHASED"} {
		t.Run(code, func(t *testing.T) {
			f := newLifecycleFixture(t)
			f.login(t)
			f.app.Close()
			f.mode = code
			if _, err := f.app.Restore(context.Background()); err == nil {
				t.Fatal("merchant denial ignored")
			}
			if len(f.runtimes) != 1 {
				t.Fatal("SDK restored after authoritative denial")
			}
			if _, err := f.store.Load(context.Background(), f.app.key); !errors.Is(err, ErrNoSession) {
				t.Fatal("denied session persisted")
			}
			if _, err := os.Stat(filepath.Dir(f.configs[0].CachePath)); !os.IsNotExist(err) {
				t.Fatal("denied account cache retained")
			}
		})
	}
}

func TestPendingRetryRetainsRememberedSession(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	f.app.Close()
	f.mode = "ENTITLEMENT_PENDING"
	before := f.credentials
	if _, err := f.app.Restore(context.Background()); err == nil {
		t.Fatal("pending authorized")
	}
	if f.credentials-before != 3 || len(f.runtimes) != 1 {
		t.Fatal("unbounded retry or pending fallback")
	}
	if _, err := f.store.Load(context.Background(), f.app.key); err != nil {
		t.Fatal("pending unnecessarily signed out", err)
	}
}

func TestLogoutClearsLocalStateEvenWithCanceledContext(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	f.app.Close()
	// A fresh process has not loaded any account; cleanup still has to work.
	f.app.record = rememberedSession{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.app.Logout(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation", err)
	}
	if _, err := f.store.Load(context.Background(), f.app.key); !errors.Is(err, ErrNoSession) {
		t.Fatal("logout retained bearer")
	}
	if _, err := os.Stat(filepath.Dir(f.configs[0].CachePath)); !os.IsNotExist(err) {
		t.Fatal("logout retained SDK credential")
	}
	if err := f.app.RunFeature("export", func() error { return nil }); !errors.Is(err, ErrNotStarted) {
		t.Fatal("logout retained authorization")
	}
}

func TestSessionExpiryAndAccountMismatch(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{true: "account", false: "expiry"}[mismatch], func(t *testing.T) {
			f := newLifecycleFixture(t)
			f.login(t)
			f.app.Close()
			if mismatch {
				f.user = "account-b"
			} else {
				f.app.record.Session.ExpiresAt = time.Now().Add(-time.Second)
				if err := f.app.save(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			_, err := f.app.Restore(context.Background())
			if mismatch && !errors.Is(err, ErrSessionMismatch) || !mismatch && !errors.Is(err, ErrSessionExpired) {
				t.Fatal(err)
			}
			if len(f.runtimes) != 1 {
				t.Fatal("invalid session opened SDK")
			}
		})
	}
}

func TestReleaseAndAccountCacheIsolation(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	first := f.configs[0].CachePath
	if err := f.app.ReleaseDevice(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.runtimes[0].released != 1 || f.logouts != 1 {
		t.Fatal("explicit release did not deactivate and revoke")
	}
	f.user = "account-b"
	f.login(t)
	if first == f.configs[1].CachePath {
		t.Fatal("accounts share SDK cache")
	}
	copyConfig := f.app.config
	copyConfig.Environment = "live"
	other, err := NewApplication(copyConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Restore(context.Background()); !errors.Is(err, ErrNoSession) {
		t.Fatal("environment reused sandbox session")
	}
	if other.cachePath(f.user) == f.app.cachePath(f.user) {
		t.Fatal("environments share SDK cache")
	}
}

func TestCredentialRetryBoundAndCancellation(t *testing.T) {
	f := newLifecycleFixture(t)
	f.mode = "ENTITLEMENT_PENDING"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.app.client.CredentialWithRetry(ctx, testProduct, "sandbox", RetryPolicy{}); !errors.Is(err, context.Canceled) || f.credentials != 0 {
		t.Fatal("canceled retry performed network work", err)
	}
	_, err := f.app.client.CredentialWithRetry(context.Background(), testProduct, "sandbox", RetryPolicy{MaxAttempts: 3, InitialDelay: time.Nanosecond, MaxDelay: time.Millisecond})
	var api *APIError
	if !errors.As(err, &api) || api.Code != "ENTITLEMENT_PENDING" || f.credentials != 3 {
		t.Fatal("pending retry is not bounded", err)
	}
	f.mode = "ENTITLEMENT_EXPIRED"
	f.credentials = 0
	_, err = f.app.client.CredentialWithRetry(context.Background(), testProduct, "sandbox", RetryPolicy{})
	if err == nil || f.credentials != 1 {
		t.Fatal("permanent error retried")
	}
}

func TestTransportErrorAndRetryAfter(t *testing.T) {
	c, _ := New("https://merchant.example", false)
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Header: http.Header{"Retry-After": []string{"120"}}, Body: io.NopCloser(strings.NewReader("<html>upstream temporarily down</html>")), Request: r}, nil
	})
	_, err := c.CurrentUser(context.Background())
	var api *APIError
	if !errors.As(err, &api) || api.RetryAfter != 120*time.Second || !ServiceUnavailable(err) {
		t.Fatal("proxy failure lost status or retry delay", err)
	}
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.CurrentUser(ctx)
	if !errors.Is(err, context.Canceled) || ServiceUnavailable(err) {
		t.Fatal("cancellation enabled fallback", err)
	}
	if !ServiceUnavailable(&TransportError{Cause: context.DeadlineExceeded}) || ServiceUnavailable(context.DeadlineExceeded) {
		t.Fatal("HTTP timeout confused with caller deadline")
	}
}

func TestUnsafeResponseErrorCodeIsNeverDisplayed(t *testing.T) {
	for _, code := range []string{"lv_lic_private-credential", "ERROR\nBearer private-token", strings.Repeat("A", 65)} {
		c, _ := New("https://merchant.example", false)
		c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
			body, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code}, "request_id": "Bearer private-token\n"})
			return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
		})
		_, err := c.CurrentUser(context.Background())
		var api *APIError
		if !errors.As(err, &api) || api.Code != "HTTP_ERROR" || api.RequestID != "" || err.Error() != "Launch: HTTP_ERROR" {
			t.Fatal("unsafe error response was retained", err)
		}
		if (&APIError{Code: code}).Error() != "Launch: HTTP_ERROR" {
			t.Fatal("manually constructed error bypassed redaction")
		}
	}
}

func TestPendingRetryAfterAndCanceledWait(t *testing.T) {
	c, _ := New("https://merchant.example", false)
	calls := 0
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 403, Header: http.Header{"Retry-After": []string{"30"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"ENTITLEMENT_PENDING"}}`)), Request: r}, nil
	})
	if _, err := c.CredentialWithRetry(context.Background(), testProduct, "sandbox", RetryPolicy{}); err == nil || calls != 1 {
		t.Fatal("long Retry-After shortened or ignored")
	}
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"code":"ENTITLEMENT_PENDING"}}`)), Request: r}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := c.CredentialWithRetry(ctx, testProduct, "sandbox", RetryPolicy{}); !errors.Is(err, context.DeadlineExceeded) || calls != 2 {
		t.Fatal("canceled retry timer kept polling", err)
	}
}
