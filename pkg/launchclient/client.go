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
	"strings"
	"time"
)

type APIError struct {
	Code      string
	Status    int
	RequestID string
}

func (e *APIError) Error() string { return "Launch: " + e.Code }

type User struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Verified bool   `json:"verified"`
}
type Credential struct {
	ProductID  string     `json:"product_id"`
	LicenseKey string     `json:"license_key"`
	ValidUntil *time.Time `json:"valid_until"`
	Version    uint64     `json:"version"`
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
		return errors.New("merchant service is unavailable")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return errors.New("invalid merchant response")
	}
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return errors.New("invalid merchant response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &APIError{envelope.Error.Code, response.StatusCode, envelope.RequestID}
	}
	if out != nil && json.Unmarshal(envelope.Data, out) != nil {
		return errors.New("invalid merchant response")
	}
	return nil
}
func (c *Client) Login(ctx context.Context, email, password string) (User, error) {
	var grant struct {
		AccessToken string `json:"access_token"`
		User        User   `json:"user"`
	}
	err := c.call(ctx, "POST", "/auth/login", map[string]string{"email": email, "password": password, "kind": "desktop"}, &grant)
	if err != nil {
		return User{}, err
	}
	if grant.AccessToken == "" || grant.User.ID == "" {
		return User{}, errors.New("merchant did not issue a software session")
	}
	c.token = grant.AccessToken
	return grant.User, nil
}
func (c *Client) CurrentUser(ctx context.Context) (User, error) {
	var result struct {
		User User `json:"user"`
	}
	err := c.call(ctx, "GET", "/me", nil, &result)
	return result.User, err
}
func (c *Client) Credential(ctx context.Context, product, environment string) (Credential, error) {
	if strings.ContainsAny(product, "/?#") || len(product) != 36 {
		return Credential{}, errors.New("invalid product identifier")
	}
	var result Credential
	err := c.call(ctx, "POST", "/software/products/"+product+"/credential", map[string]string{"environment": environment}, &result)
	if err == nil && (result.ProductID != product || result.LicenseKey == "") {
		return Credential{}, errors.New("credential belongs to a different product")
	}
	return result, err
}
func (c *Client) SignOut(ctx context.Context) error {
	err := c.call(ctx, "POST", "/auth/logout", struct{}{}, nil)
	c.token = ""
	return err
}
