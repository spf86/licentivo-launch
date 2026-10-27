package launchclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAcceptedLogoutRetainsItsAccountTaskAcrossRestart(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	status := http.StatusAccepted
	base := f.app.client.http.Transport
	f.app.client.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/auth/logout" {
			return base.RoundTrip(r)
		}
		f.paths = append(f.paths, r.URL.Path)
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":{"accepted":true}}`)), Request: r}, nil
	})
	if err := f.app.Logout(context.Background()); !errors.Is(err, ErrAccountLogoutPending) {
		t.Fatal("202 discarded the unconfirmed logout task", err)
	}
	if f.app.runtime != nil {
		t.Fatal("pending logout retained a running SDK")
	}
	f.app.Close()
	restarted, err := NewApplication(f.app.config)
	if err != nil {
		t.Fatal(err)
	}
	restarted.client.http = f.app.client.http
	t.Cleanup(restarted.Close)
	before := len(f.paths)
	if _, err := restarted.Restore(context.Background()); !errors.Is(err, ErrAccountLogoutPending) {
		t.Fatal("restart did not retry the accepted logout", err)
	}
	status = http.StatusOK
	if _, err := restarted.Restore(context.Background()); !errors.Is(err, ErrNoSession) {
		t.Fatal("confirmed logout did not finish", err)
	}
	for _, path := range f.paths[before:] {
		if path != "/api/v1/auth/logout" {
			t.Fatal("unconfirmed logout was restored as a login", path)
		}
	}
	if _, err := f.store.Load(context.Background(), restarted.key); !errors.Is(err, ErrNoSession) {
		t.Fatal("confirmed account task was retained", err)
	}
}

func TestMalformedLogoutSuccessRetainsAccountTask(t *testing.T) {
	for name, invalid := range map[string]string{
		"missing_data": `{"error":{"code":"UNEXPECTED"}}`,
		"null_data":    `{"data":null}`,
		"array_data":   `{"data":[]}`,
		"null_body":    `null`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newLifecycleFixture(t)
			f.login(t)
			body := invalid
			base := f.app.client.http.Transport
			f.app.client.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/api/v1/auth/logout" {
					return base.RoundTrip(r)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			if err := f.app.Logout(context.Background()); !errors.Is(err, ErrAccountLogoutPending) {
				t.Fatal("malformed success discarded account logout", err)
			}
			if f.app.runtime != nil {
				t.Fatal("malformed logout retained local execution")
			}
			body = `{"data":{"completed":true}}`
			if err := f.app.Logout(context.Background()); err != nil {
				t.Fatal("valid confirmation did not finish the pending logout", err)
			}
			if _, err := f.store.Load(context.Background(), f.app.key); !errors.Is(err, ErrNoSession) {
				t.Fatal("confirmed account task remained", err)
			}
		})
	}
}

func TestTransportPolicyDenialPreservesTheAccountForLogout(t *testing.T) {
	for _, code := range []string{"SDK_HTTP_DISABLED", "SDK_HTTPS_REQUIRED"} {
		t.Run(code, func(t *testing.T) {
			f := newLifecycleFixture(t)
			f.login(t)
			saved, err := f.store.Load(context.Background(), f.app.key)
			if err != nil {
				t.Fatal(err)
			}
			base := f.app.client.http.Transport
			f.app.client.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/api/v1/me" {
					return base.RoundTrip(r)
				}
				return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"` + code + `"}}`)), Request: r}, nil
			})
			var api *APIError
			if _, err := f.app.Restore(context.Background()); !errors.As(err, &api) || api.Code != code {
				t.Fatal("lost the transport policy error", err)
			}
			current, err := f.store.Load(context.Background(), f.app.key)
			if err != nil || !bytes.Equal(current, saved) {
				t.Fatal("transport policy denial discarded a valid account", err)
			}
			if f.app.runtime != nil || len(f.configs) != 1 {
				t.Fatal("transport policy denial started a licensing runtime")
			}
			if err := f.app.Logout(context.Background()); err != nil {
				t.Fatal("transport policy denial prevented account revocation", err)
			}
			if _, err := f.store.Load(context.Background(), f.app.key); !errors.Is(err, ErrNoSession) {
				t.Fatal("confirmed logout retained the saved account", err)
			}
		})
	}
}

func TestMalformedCurrentUserPreservesAccountForLogout(t *testing.T) {
	for _, body := range []string{
		`{"data":{}}`, `{"data":{"user":null}}`, `{"data":{"user":{}}}`,
		`{"data":{"user":{"id":""}}}`, `{"data":{"user":{"id":null}}}`,
		`{"data":{"user":{"id":42}}}`, `{"data":{"user":[]}}`, `{"data":{"user":"invalid"}}`,
	} {
		t.Run(body, func(t *testing.T) {
			f := newLifecycleFixture(t)
			f.login(t)
			saved, err := f.store.Load(context.Background(), f.app.key)
			if err != nil {
				t.Fatal(err)
			}
			token := f.app.record.Session.AccessToken
			base := f.app.client.http.Transport
			logoutBearer := ""
			f.app.client.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/api/v1/me" {
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				}
				if r.URL.Path == "/api/v1/auth/logout" {
					logoutBearer = r.Header.Get("Authorization")
				}
				return base.RoundTrip(r)
			})
			if _, err := f.app.Restore(context.Background()); !errors.Is(err, ErrInvalidResponse) {
				t.Fatal("malformed user was treated as an account change", err)
			}
			retained, err := f.store.Load(context.Background(), f.app.key)
			if err != nil || !bytes.Equal(saved, retained) || f.app.runtime != nil || len(f.runtimes) != 1 {
				t.Fatal("malformed user discarded account or started runtime", err)
			}
			if err := f.app.RunFeature("export", func() error { t.Fatal("failed restore executed work"); return nil }); !errors.Is(err, ErrNotStarted) {
				t.Fatal("failed restore retained local authorization", err)
			}
			if err := f.app.Logout(context.Background()); err != nil || logoutBearer != "Bearer "+token {
				t.Fatal("malformed user prevented explicit session revocation", err)
			}
		})
	}
}

func TestDifferentCurrentUserStillClearsAccount(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	f.user = "account-b"
	if _, err := f.app.Restore(context.Background()); !errors.Is(err, ErrSessionMismatch) {
		t.Fatal("different account was accepted", err)
	}
	if _, err := f.store.Load(context.Background(), f.app.key); !errors.Is(err, ErrNoSession) {
		t.Fatal("different account retained the old login", err)
	}
}
