package launchclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type failingAccountStore struct {
	SessionStore
	failSave, failDelete func(string) bool
}

func (s *failingAccountStore) Save(ctx context.Context, key string, raw []byte) error {
	if s.failSave != nil && s.failSave(key) {
		return errors.New("atomic account store save failed")
	}
	return s.SessionStore.Save(ctx, key, raw)
}
func (s *failingAccountStore) Delete(ctx context.Context, key string) error {
	if s.failDelete != nil && s.failDelete(key) {
		return errors.New("account store delete failed")
	}
	return s.SessionStore.Delete(ctx, key)
}

func TestAccountLogoutStoreFailureDoesNotRestoreOldLogin(t *testing.T) {
	for _, mode := range []string{"all_saves_fail", "active_save_and_delete_fail", "queue_save_fails"} {
		t.Run(mode, func(t *testing.T) {
			f := newLifecycleFixture(t)
			f.login(t)
			store := &failingAccountStore{SessionStore: f.store}
			store.failSave = func(key string) bool {
				return mode == "all_saves_fail" || mode == "active_save_and_delete_fail" && key == f.app.key || mode == "queue_save_fails" && key == f.app.queueKey()
			}
			store.failDelete = func(key string) bool { return mode == "active_save_and_delete_fail" && key == f.app.key }
			f.app.store, f.app.config.Store = store, store
			f.mode = "offline"
			err := f.app.Logout(context.Background())
			if !errors.Is(err, ErrAccountLogoutPending) {
				t.Fatal(err)
			}
			if mode == "all_saves_fail" && !errors.Is(err, ErrAccountLogoutNotPersisted) {
				t.Fatal("lost retry task was not reported", err)
			}
			f.app.Close()
			store.failSave, store.failDelete = nil, nil
			restarted, err := NewApplication(f.app.config)
			if err != nil {
				t.Fatal(err)
			}
			restarted.client.http = f.app.client.http
			t.Cleanup(restarted.Close)
			before := len(f.paths)
			_, err = restarted.Restore(context.Background())
			if mode == "all_saves_fail" {
				if !errors.Is(err, ErrNoSession) {
					t.Fatal("old login survived failed persistence", err)
				}
			} else if !errors.Is(err, ErrAccountLogoutPending) {
				t.Fatal(err)
			}
			for _, path := range f.paths[before:] {
				if path != "/api/v1/auth/logout" {
					t.Fatal("logout restored login", path)
				}
			}
			if restarted.runtime != nil {
				t.Fatal("interrupted logout started a runtime")
			}
		})
	}
}

func TestAccountLogoutReportsTotalStorageFailure(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	f.app.store = &failingAccountStore{SessionStore: f.store, failSave: func(string) bool { return true }, failDelete: func(string) bool { return true }}
	f.app.config.Store = f.app.store
	f.mode = "offline"
	err := f.app.Logout(context.Background())
	if !errors.Is(err, ErrAccountLogoutPending) || !errors.Is(err, ErrAccountLogoutNotPersisted) {
		t.Fatal("total persistence failure reported as durable logout", err)
	}
	if f.app.runtime != nil {
		t.Fatal("storage failure retained local execution")
	}
	restarted, err := NewApplication(f.app.config)
	if err != nil {
		t.Fatal(err)
	}
	restarted.client.http = f.app.client.http
	t.Cleanup(restarted.Close)
	before := len(f.paths)
	if _, err = restarted.Restore(context.Background()); !errors.Is(err, ErrAccountLogoutPending) {
		t.Fatal("total vault failure restored the account", err)
	}
	for _, path := range f.paths[before:] {
		if path != "/api/v1/auth/logout" {
			t.Fatal("fenced account fetched identity", path)
		}
	}
}

func TestAccountLogoutLockTimeoutKeepsAccountExitIntent(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	unlock, err := f.app.lockAccount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() { once.Do(unlock) }
	defer release()
	if err = f.app.Logout(context.Background()); !errors.Is(err, ErrAccountLogoutNotPersisted) {
		t.Fatal("expected account lock failure", err)
	}
	release()
	f.mode = "offline"
	restarted, err := NewApplication(f.app.config)
	if err != nil {
		t.Fatal(err)
	}
	restarted.client.http = f.app.client.http
	t.Cleanup(restarted.Close)
	before := len(f.paths)
	if _, err = restarted.Restore(context.Background()); !errors.Is(err, ErrAccountLogoutPending) {
		t.Fatal("lock timeout restored old login", err)
	}
	for _, path := range f.paths[before:] {
		if path != "/api/v1/auth/logout" {
			t.Fatal("fenced account fetched identity", path)
		}
	}
}

func TestOldAccountLogoutPreservesNewLoginAndRetriesOwnQueue(t *testing.T) {
	f := newLifecycleFixture(t)
	underlying := f.app.client.http.Transport
	logins := 0
	oldToken := ""
	blockOld := false
	f.app.client.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/auth/logout" && blockOld && r.Header.Get("Authorization") == "Bearer "+oldToken {
			return nil, errors.New("old account logout endpoint unavailable")
		}
		if r.URL.Path != "/api/v1/auth/login" {
			return underlying.RoundTrip(r)
		}
		logins++
		value := Session{AccessToken: fmt.Sprintf("account-%d", logins), User: User{ID: f.user}, ExpiresAt: time.Now().Add(time.Hour)}
		raw, _ := json.Marshal(map[string]any{"data": value})
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
	})
	f.login(t)
	oldToken = f.app.record.Session.AccessToken
	newApp, err := NewApplication(f.app.config)
	if err != nil {
		t.Fatal(err)
	}
	newApp.client.http = f.app.client.http
	t.Cleanup(newApp.Close)
	if _, err = newApp.Login(context.Background(), "a", "b"); err != nil {
		t.Fatal(err)
	}
	newToken := newApp.record.Session.AccessToken
	blockOld = true
	if err = f.app.Logout(context.Background()); !errors.Is(err, ErrAccountLogoutPending) {
		t.Fatal(err)
	}
	active, err := newApp.load(context.Background())
	if err != nil || active.Session.AccessToken != newToken || active.LogoutPending {
		t.Fatal("old instance changed new login", err)
	}
	queue, err := newApp.pendingAccounts(context.Background())
	if err != nil || len(queue) != 1 || queue[0].Session.AccessToken != oldToken {
		t.Fatal("old session retry was lost", err)
	}
	blockOld = false
	f.app.Close()
	restarted, err := NewApplication(f.app.config)
	if err != nil {
		t.Fatal(err)
	}
	restarted.client.http = f.app.client.http
	t.Cleanup(restarted.Close)
	before := len(f.runtimes)
	if _, err = restarted.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if restarted.record.Session.AccessToken != newToken || len(f.runtimes) != before+1 {
		t.Fatal("old task reopened the newer licensing cache")
	}
	queue, err = restarted.pendingAccounts(context.Background())
	if err != nil || len(queue) != 0 {
		t.Fatal("old session retry did not complete", err)
	}
	if err = f.app.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	active, err = restarted.load(context.Background())
	// Close retains the old pending record in memory, so its completed retry
	// must still never remove the newer login.
	if err != nil || active.Session.AccessToken != newToken {
		t.Fatal("401/success cleanup removed a newer login", err)
	}
	if err = f.app.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	active, err = restarted.load(context.Background())
	if err != nil || active.Session.AccessToken != newToken {
		t.Fatal("completed old logout adopted the newer account", err)
	}
}
