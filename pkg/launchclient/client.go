// Package launchclient authenticates software against a merchant's Launch
// instance. Actual feature authorization still belongs to the Licentivo SDK.
package launchclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type APIError struct {
	Code       string
	Status     int
	RequestID  string
	RetryAfter time.Duration
}

func (e *APIError) Error() string { return "Launch: " + safeCode(e.Code) }

var codePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
var requestIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._:-]{1,128}$`)
var productIDPattern = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

func safeCode(code string) string {
	if codePattern.MatchString(code) {
		return code
	}
	return "HTTP_ERROR"
}
func safeRequestID(id string) string {
	if requestIDPattern.MatchString(id) {
		return id
	}
	return ""
}

// TransportError preserves cancellation and network causes without including
// request URLs, passwords, or bearer tokens in its display text.
type TransportError struct{ Cause error }

func (e *TransportError) Error() string { return "merchant service is unavailable" }
func (e *TransportError) Unwrap() error { return e.Cause }

var ErrInvalidResponse = errors.New("invalid merchant response")

// ServiceUnavailable is deliberately narrow: authentication and entitlement
// denials, malformed responses and canceled requests must never trigger offline fallback.
func ServiceUnavailable(err error) bool {
	var transport *TransportError
	if errors.As(err, &transport) {
		return !errors.Is(err, context.Canceled)
	}
	var api *APIError
	return errors.As(err, &api) && api.Status >= 500 && api.Status <= 599
}

type User struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Verified bool   `json:"verified"`
}

// Identity is an ephemeral account assertion and public routing context.
// It is not a license, grant, feature list or authorization decision.
type Identity struct {
	ProductID          string    `json:"product_id"`
	LicentivoProductID string    `json:"licentivo_product_id"`
	LaunchConnectionID string    `json:"launch_connection_id"`
	Environment        string    `json:"environment"`
	IdentityToken      string    `json:"identity_token,omitempty"`
	ExpiresAt          time.Time `json:"expires_at"`
}
type Credential struct {
	ProductID  string     `json:"product_id"`
	LicenseKey string     `json:"license_key"`
	ValidUntil *time.Time `json:"valid_until"`
	Version    uint64     `json:"version"`
}

// Session contains secrets. Store it only through a protected SessionStore;
// never print or serialize it into application logs.
type Session struct {
	AccessToken string    `json:"access_token"`
	User        User      `json:"user"`
	ExpiresAt   time.Time `json:"expires_at"`
}
type Client struct {
	base  string
	http  *http.Client
	token string
}

func New(origin string, allowLocalHTTP bool) (*Client, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return nil, errors.New("merchant URL must be an origin")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local && allowLocalHTTP) {
		return nil, errors.New("HTTPS is required")
	}
	return &Client{base: strings.TrimRight(origin, "/"), http: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// WithToken is for a trusted backend receiving the caller's bearer token. It
// does not imply trust in that caller's user ID or product entitlements.
func (c *Client) WithToken(token string) *Client { clone := *c; clone.token = token; return &clone }
func (c *Client) call(ctx context.Context, method, path string, input, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var raw []byte
	var err error
	if input != nil {
		raw, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	r, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v1"+path, bytes.NewReader(raw))
	if err != nil {
		return errors.New("invalid Launch request")
	}
	r.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		r.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.http.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &TransportError{Cause: err}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &TransportError{Cause: err}
	}
	if len(body) > 1<<20 {
		return ErrInvalidResponse
	}
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	decodeErr := json.Unmarshal(body, &envelope)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &APIError{Code: safeCode(envelope.Error.Code), Status: response.StatusCode, RequestID: safeRequestID(envelope.RequestID), RetryAfter: retryAfter(response.Header.Get("Retry-After"), time.Now())}
	}
	if decodeErr != nil || out != nil && json.Unmarshal(envelope.Data, out) != nil {
		return ErrInvalidResponse
	}
	return nil
}
func (c *Client) Login(ctx context.Context, email, password string) (User, error) {
	grant, err := c.LoginSession(ctx, email, password)
	return grant.User, err
}
func (c *Client) LoginSession(ctx context.Context, email, password string) (Session, error) {
	var grant Session
	err := c.call(ctx, "POST", "/auth/login", map[string]string{"email": email, "password": password, "kind": "desktop"}, &grant)
	if err != nil {
		return Session{}, err
	}
	if grant.AccessToken == "" || grant.User.ID == "" {
		return Session{}, errors.New("merchant did not issue a software session")
	}
	c.token = grant.AccessToken
	return grant, nil
}
func (c *Client) CurrentUser(ctx context.Context) (User, error) {
	var result struct {
		User User `json:"user"`
	}
	err := c.call(ctx, "GET", "/me", nil, &result)
	return result.User, err
}

// Credential is retained only to report the removed legacy protocol.
func (c *Client) Credential(ctx context.Context, product, environment string) (Credential, error) {
	return Credential{}, errors.New("long-term license credentials are no longer supplied by Launch; use Identity")
}
func (c *Client) Identity(ctx context.Context, product, environment string) (Identity, error) {
	if !productIDPattern.MatchString(product) || (environment != "live" && environment != "sandbox") {
		return Identity{}, errors.New("invalid identity context")
	}
	var out Identity
	e := c.call(ctx, "POST", "/software/products/"+product+"/identity", map[string]string{"environment": environment}, &out)
	if e == nil && (out.ProductID != product || out.Environment != environment || !productIDPattern.MatchString(out.LicentivoProductID) || !productIDPattern.MatchString(out.LaunchConnectionID) || len(out.IdentityToken) < 32 || len(out.IdentityToken) > 12288 || !out.ExpiresAt.After(time.Now())) {
		return Identity{}, ErrInvalidResponse
	}
	return out, e
}

func retryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if deadline, err := http.ParseTime(value); err == nil && deadline.After(now) {
		return deadline.Sub(now)
	}
	return 0
}
func (c *Client) SignOut(ctx context.Context) error {
	err := c.call(ctx, "POST", "/auth/logout", struct{}{}, nil)
	c.token = ""
	return err
}
