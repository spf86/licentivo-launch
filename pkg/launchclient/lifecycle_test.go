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
const testRemote = "33333333-3333-4333-8333-333333333333"
const testConnection = "22222222-2222-4222-8222-222222222222"

type fakeRuntime struct {
	path                      string
	started, released, closed int
}

func (r *fakeRuntime) Start(context.Context, func(error)) error { r.started++; return nil }
func (r *fakeRuntime) RunFeature(f string, a func() error) error {
	if f != "export" {
		return errors.New("official SDK denied")
	}
	return a()
}
func (r *fakeRuntime) Deactivate(context.Context) error {
	r.released++
	return os.WriteFile(r.path+".logout.json", []byte("SDK-owned unlink-only journal"), 0600)
}
func (r *fakeRuntime) Close() { r.closed++ }

type lifecycleFixture struct {
	app        *Application
	store      *MemoryStore
	mode, user string
	runtimes   []*fakeRuntime
	configs    []RuntimeConfig
	paths      []string
}

func newLifecycleFixture(t *testing.T) *lifecycleFixture {
	t.Helper()
	f := &lifecycleFixture{store: &MemoryStore{}, user: "account-a"}
	a, e := NewApplication(Config{Origin: "https://merchant.example", ProductID: testProduct, Environment: "sandbox", Store: f.store, CacheDir: t.TempDir(), RuntimeFactory: func(c RuntimeConfig) (Runtime, error) {
		r := &fakeRuntime{path: c.CachePath}
		f.configs = append(f.configs, c)
		f.runtimes = append(f.runtimes, r)
		return r, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	f.app = a
	a.client.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		f.paths = append(f.paths, r.URL.Path)
		if f.mode == "offline" {
			return nil, errors.New("offline")
		}
		if r.Context().Err() != nil {
			return nil, r.Context().Err()
		}
		status := 200
		var data any
		switch r.URL.Path {
		case "/api/v1/auth/login":
			data = Session{AccessToken: "account-secret-" + hashScope(a.config.CacheDir) + "-" + f.user, User: User{ID: f.user}, ExpiresAt: time.Now().Add(time.Hour)}
		case "/api/v1/me":
			data = map[string]any{"user": User{ID: f.user}}
		case "/api/v1/auth/logout":
			data = map[string]bool{"signed_out": true}
		default:
			if strings.HasSuffix(r.URL.Path, "/credential") {
				t.Fatal("legacy key request")
			}
			data = Identity{ProductID: testProduct, LicentivoProductID: testRemote, LaunchConnectionID: testConnection, Environment: "sandbox", IdentityToken: strings.Repeat("ephemeral-identity-", 4), ExpiresAt: time.Now().Add(time.Minute)}
		}
		raw, _ := json.Marshal(map[string]any{"data": data})
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
	})
	t.Cleanup(a.Close)
	return f
}
func (f *lifecycleFixture) login(t *testing.T) {
	t.Helper()
	if _, e := f.app.Login(context.Background(), "a@example.invalid", "password"); e != nil {
		t.Fatal(e)
	}
}
func TestIdentityIsEphemeralAndSDKDecidesFeatures(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	raw, e := f.store.Load(context.Background(), f.app.key)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), "ephemeral-identity") || strings.Contains(string(raw), "license_key") {
		t.Fatal("account store retained licensing secrets")
	}
	if f.configs[0].Identity.IdentityToken == "" {
		t.Fatal("identity not handed to official SDK")
	}
	called := false
	if e := f.app.RunFeature("not-owned", func() error { called = true; return nil }); e == nil || called {
		t.Fatal("SDK denial bypassed")
	}
}
func TestLogoutReleasesAndRetainsSDKJournal(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	path := f.configs[0].CachePath
	if e := f.app.Logout(context.Background()); e != nil {
		t.Fatal(e)
	}
	if f.runtimes[0].released != 1 {
		t.Fatal("logout did not call SDK logout")
	}
	if _, e := os.Stat(path + ".logout.json"); e != nil {
		t.Fatal("Launch deleted pending SDK journal", e)
	}
	if _, e := f.store.Load(context.Background(), f.app.key); !errors.Is(e, ErrNoSession) {
		t.Fatal("account not cleared")
	}
	if e := f.app.RunFeature("export", func() error { return nil }); !errors.Is(e, ErrNotStarted) {
		t.Fatal("local work retained")
	}
}
func TestCloseKeepsAccountAndDoesNotRelease(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	f.app.Close()
	if f.runtimes[0].released != 0 {
		t.Fatal("ordinary close released device")
	}
	if _, e := f.store.Load(context.Background(), f.app.key); e != nil {
		t.Fatal(e)
	}
	if _, e := f.app.Restore(context.Background()); e != nil {
		t.Fatal(e)
	}
}
func TestLaunchOutageNeverDecidesOfflineAuthorization(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	f.app.Close()
	f.mode = "offline"
	if _, e := f.app.Restore(context.Background()); e == nil {
		t.Fatal("Launch fabricated offline authorization")
	}
	if len(f.runtimes) != 1 {
		t.Fatal("Launch chose offline startup")
	}
}
func TestLogoutAfterRestartReopensSDKForRevocation(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	path := f.configs[0].CachePath
	f.app.Close()
	f.app.record = rememberedSession{}
	if e := f.app.Logout(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(f.runtimes) != 2 || f.runtimes[1].released != 1 || f.configs[1].Identity.IdentityToken != "" {
		t.Fatal("restart logout did not use SDK-owned credential")
	}
	if _, e := os.Stat(filepath.Dir(path)); e != nil {
		t.Fatal("SDK directory deleted")
	}
}
func TestCanceledLogoutQueuesAccountAndStopsRuntime(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = f.app.Logout(ctx)
	raw, e := f.store.Load(context.Background(), f.app.key)
	var pending rememberedSession
	if e != nil || json.Unmarshal(raw, &pending) != nil || !pending.LogoutPending {
		t.Fatal("canceled logout lost its pending account task", e)
	}
	if f.app.runtime != nil {
		t.Fatal("canceled logout retained runtime")
	}
}

func TestAccountLogoutRetriesAfterRestartWithoutRestoringLogin(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	f.mode = "offline"
	if e := f.app.Logout(context.Background()); !errors.Is(e, ErrAccountLogoutPending) {
		t.Fatal("missing pending account status", e)
	}
	if e := f.app.RunFeature("export", func() error { t.Fatal("signed-out callback executed"); return nil }); !errors.Is(e, ErrNotStarted) {
		t.Fatal("logout retained local execution", e)
	}
	f.app.Close()
	restarted, e := NewApplication(f.app.config)
	if e != nil {
		t.Fatal(e)
	}
	restarted.client.http = f.app.client.http
	t.Cleanup(restarted.Close)
	before := len(f.paths)
	if _, e := restarted.Restore(context.Background()); !errors.Is(e, ErrAccountLogoutPending) {
		t.Fatal("restart did not retry the pending logout", e)
	}
	for _, path := range f.paths[before:] {
		if path != "/api/v1/auth/logout" {
			t.Fatal("pending login was reused", path)
		}
	}
	f.mode = ""
	before = len(f.paths)
	if e := restarted.Logout(context.Background()); e != nil {
		t.Fatal("recovered logout did not complete", e)
	}
	if len(f.paths) <= before || f.paths[before] != "/api/v1/auth/logout" {
		t.Fatal("no account revocation request after recovery")
	}
	if _, e := f.store.Load(context.Background(), restarted.key); !errors.Is(e, ErrNoSession) {
		t.Fatal("confirmed logout task was not removed", e)
	}
	if _, e := restarted.Restore(context.Background()); !errors.Is(e, ErrNoSession) {
		t.Fatal("signed-out account restored", e)
	}
}
func TestAccountSwitchReleasesOldDevice(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	path := f.configs[0].CachePath
	f.user = "account-b"
	f.login(t)
	if f.runtimes[0].released != 1 || path == f.configs[1].CachePath {
		t.Fatal("account isolation/release failed")
	}
}
func TestSessionExpiryDoesNotOpenSDK(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	f.app.record.Session.ExpiresAt = time.Now().Add(-time.Second)
	if e := f.app.save(context.Background()); e != nil {
		t.Fatal(e)
	}
	f.app.Close()
	if _, e := f.app.Restore(context.Background()); !errors.Is(e, ErrSessionExpired) {
		t.Fatal(e)
	}
	if len(f.runtimes) != 1 {
		t.Fatal("expired account opened SDK")
	}
}
