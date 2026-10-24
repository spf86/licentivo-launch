package launchclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestSoftwareAuthenticationAndErrorRedaction(t *testing.T) {
	c, err := New("https://merchant.example", false)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := ""
		code := 200
		switch r.URL.Path {
		case "/api/v1/auth/login":
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), `"kind":"desktop"`) {
				t.Fatal("website session requested")
			}
			body = `{"data":{"access_token":"customer-token","user":{"id":"user-id","email":"test@example.invalid","verified":true}}}`
		case "/api/v1/software/products/11111111-1111-4111-8111-111111111111/identity":
			if r.Header.Get("Authorization") != "Bearer customer-token" {
				t.Fatal("missing own bearer")
			}
			code = 403
			body = `{"error":{"code":"ENTITLEMENT_UNAVAILABLE","message":"do not expose raw details or credentials"},"request_id":"request-test"}`
		case "/api/v1/auth/logout":
			body = `{"data":{"signed_out":true}}`
		default:
			t.Fatal("unexpected request")
		}
		return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	user, err := c.Login(context.Background(), "test@example.invalid", "test-only-password")
	if err != nil || user.ID != "user-id" {
		t.Fatal(err)
	}
	_, err = c.Identity(context.Background(), "11111111-1111-4111-8111-111111111111", "sandbox")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "ENTITLEMENT_UNAVAILABLE" || strings.Contains(err.Error(), "raw details") {
		t.Fatal("unsafe API error")
	}
	if err := c.SignOut(context.Background()); err != nil || c.token != "" {
		t.Fatal("sign out retained bearer")
	}
	if calls != 3 {
		t.Fatal("unexpected network retry")
	}
}
func TestOriginPolicy(t *testing.T) {
	for _, origin := range []string{"http://merchant.example", "https://user:secret@merchant.example", "https://merchant.example/path", "https://merchant.example?key=secret"} {
		if _, err := New(origin, true); err == nil {
			t.Fatal("unsafe merchant origin accepted")
		}
	}
	if _, err := New("http://127.0.0.1:18080", true); err != nil {
		t.Fatal(err)
	}
}
