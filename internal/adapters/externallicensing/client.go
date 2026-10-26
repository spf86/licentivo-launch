// Package externallicensing implements the optional merchant-owned HTTP bridge.
package externallicensing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"licentivo-launch/internal/ports"
)

type Config struct {
	BaseURL, ConnectionRef, Environment, KeyID string
	Key                                        []byte
	AllowPrivateNetwork, AllowLocalHTTP        bool
}
type Client struct {
	config Config
	base   *url.URL
	http   *http.Client
}

// APIError deliberately excludes response messages, URLs and credentials.
type APIError struct {
	Status     int
	Code       string
	RetryAfter time.Duration
}

func (e *APIError) Error() string { return "external licensing request failed" }

var ErrResponse = errors.New("invalid external licensing response")

func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.Path, "..") || !ports.ExternalReference.MatchString(cfg.ConnectionRef) || (cfg.Environment != "sandbox" && cfg.Environment != "live") || len(cfg.Key) < 32 || len(cfg.Key) > 128 || cfg.KeyID == "" || len(cfg.KeyID) > 64 || !ports.ExternalReference.MatchString(cfg.KeyID) {
		return nil, errors.New("invalid external licensing configuration")
	}
	loopback := u.Hostname() == "localhost" || (net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && cfg.Environment == "sandbox" && cfg.AllowLocalHTTP && loopback) {
		return nil, errors.New("external licensing requires HTTPS")
	}
	if cfg.AllowLocalHTTP && (cfg.Environment != "sandbox" || !loopback) {
		return nil, errors.New("HTTP exception is limited to sandbox loopback")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	// Each request gets a fresh signature nonce. Do not let Transport replay
	// a request on a reused keep-alive connection with its previous nonce.
	transport.DisableKeepAlives = true
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, errors.New("external host unavailable")
		}
		for _, entry := range ips {
			ip := entry.IP
			if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || (!cfg.AllowPrivateNetwork && !(cfg.AllowLocalHTTP && ip.IsLoopback()) && (ip.IsPrivate() || ip.IsLoopback())) {
				return nil, errors.New("external network destination is disallowed")
			}
		}
		var last error
		for _, entry := range ips {
			connection, e := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(entry.IP.String(), port))
			if e == nil {
				return connection, nil
			}
			last = e
		}
		if last == nil {
			last = errors.New("external host unavailable")
		}
		return nil, last
	}
	return &Client{cfg, u, &http.Client{Timeout: 15 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Signature signs exact bytes. A retry must keep its event body but use a new nonce.
func Signature(key []byte, keyID, timestamp, nonce, method, target string, body []byte) string {
	sum := sha256.Sum256(body)
	message := strings.Join([]string{"v1", keyID, timestamp, nonce, method, target, hex.EncodeToString(sum[:])}, "\n")
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(message))
	return "v1=" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyRequest verifies the signature/time window. Receivers must additionally
// persist nonces atomically for 600 seconds and deduplicate business event IDs.
func VerifyRequest(r *http.Request, body, key []byte, keyID string, at time.Time) bool {
	timestamp, nonce := r.Header.Get("X-Launch-Timestamp"), r.Header.Get("X-Launch-Nonce")
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	decoded, e := base64.RawURLEncoding.Strict().DecodeString(nonce)
	if err != nil || e != nil || len(decoded) < 16 || len(nonce) > 128 || len(key) < 32 || r.Header.Get("X-Launch-Key-Id") != keyID {
		return false
	}
	delta := at.Sub(time.Unix(seconds, 0))
	if delta > 300*time.Second || delta < -300*time.Second {
		return false
	}
	want := Signature(key, keyID, timestamp, nonce, r.Method, r.URL.RequestURI(), body)
	return hmac.Equal([]byte(want), []byte(r.Header.Get("X-Launch-Signature")))
}

func (c *Client) request(ctx context.Context, method, path string, body []byte, out any) error {
	u := *c.base
	u.Path += path
	r, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return ErrResponse
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	nonceText := base64.RawURLEncoding.EncodeToString(nonce)
	r.Header.Set("X-Launch-Key-Id", c.config.KeyID)
	r.Header.Set("X-Launch-Timestamp", timestamp)
	r.Header.Set("X-Launch-Nonce", nonceText)
	r.Header.Set("X-Launch-Signature", Signature(c.config.Key, c.config.KeyID, timestamp, nonceText, method, r.URL.RequestURI(), body))
	response, err := c.http.Do(r)
	if err != nil {
		return errors.New("external licensing transport unavailable")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return ErrResponse
	}
	if response.StatusCode != 200 && !(method == "POST" && response.StatusCode == 202) {
		var failure struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &failure)
		code := failure.Error.Code
		if len(code) > 96 || strings.Trim(code, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" {
			code = "EXTERNAL_REQUEST_FAILED"
		}
		delay := time.Duration(0)
		if seconds, e := strconv.Atoi(response.Header.Get("Retry-After")); e == nil && seconds > 0 {
			delay = time.Duration(min(seconds, 3600)) * time.Second
		}
		return &APIError{response.StatusCode, code, delay}
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, out) != nil {
		return ErrResponse
	}
	return nil
}
func (c *Client) Capabilities(ctx context.Context) (ports.ExternalCapabilities, error) {
	var result ports.ExternalCapabilities
	err := c.request(ctx, "GET", "/launch/v1/capabilities", nil, &result)
	if err == nil && (result.Validate() != nil || result.ConnectionRef != c.config.ConnectionRef || result.Environment != c.config.Environment) {
		err = ErrResponse
	}
	return result, err
}
func (c *Client) Submit(ctx context.Context, snapshot ports.ExternalSnapshot) (ports.ExternalReceipt, error) {
	if snapshot.Validate() != nil || snapshot.ConnectionRef != c.config.ConnectionRef || snapshot.Environment != c.config.Environment {
		return ports.ExternalReceipt{}, ErrResponse
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return ports.ExternalReceipt{}, err
	}
	var result ports.ExternalReceipt
	err = c.request(ctx, "POST", "/launch/v1/events", raw, &result)
	return result, err
}
func (c *Client) Event(ctx context.Context, id string) (ports.ExternalReceipt, error) {
	if !ports.ExternalReference.MatchString(id) {
		return ports.ExternalReceipt{}, ErrResponse
	}
	var result ports.ExternalReceipt
	err := c.request(ctx, "GET", "/launch/v1/events/"+url.PathEscape(id), nil, &result)
	return result, err
}
func (c *Client) Delivery(ctx context.Context, ref string) (ports.ExternalDelivery, error) {
	if !ports.ExternalReference.MatchString(ref) {
		return ports.ExternalDelivery{}, ErrResponse
	}
	var result ports.ExternalDelivery
	err := c.request(ctx, "GET", "/launch/v1/entitlements/"+url.PathEscape(ref)+"/delivery", nil, &result)
	return result, err
}

func ValidateReceipt(receipt ports.ExternalReceipt, snapshot ports.ExternalSnapshot) error {
	raw, _ := json.Marshal(snapshot)
	sum := sha256.Sum256(raw)
	if receipt.EventID != snapshot.EventID || receipt.ConnectionRef != snapshot.ConnectionRef || receipt.Environment != snapshot.Environment || receipt.EntitlementRef != snapshot.EntitlementRef || receipt.Version != snapshot.Version || receipt.PayloadSHA256 != fmt.Sprintf("%x", sum) {
		return ErrResponse
	}
	switch receipt.Status {
	case "pending", "applied", "rejected", "manual_review":
		return nil
	}
	return ErrResponse
}
