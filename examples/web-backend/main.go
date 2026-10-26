// A bearer-authenticated backend with one serialized SDK lifecycle per account.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"licentivo-launch/pkg/launchclient"
)

type runtimeEntry struct {
	mu            sync.Mutex
	sdk           launchclient.Runtime
	assertionHash [32]byte
	refs          int
	used          time.Time
}
type runtimePool struct {
	mu       sync.Mutex
	entries  map[string]*runtimeEntry
	template launchclient.ProductConfig
	root     string
	ctx      context.Context
}

var errRuntimeBusy = errors.New("runtime capacity reached")
var errFeatureDenied = errors.New("SDK denied the protected feature")

// Bounded, private per-account cache; each SDK owns a single heartbeat loop.
// Production deployments should size capacity and persist device ownership for
// their deployment model. Sharing a cache directory across processes is unsupported.
func (p *runtimePool) use(scope string, identity launchclient.Identity, action func(launchclient.Runtime) error) error {
	p.mu.Lock()
	entry := p.entries[scope]
	if entry == nil {
		if len(p.entries) >= 64 {
			var oldestKey string
			var oldest *runtimeEntry
			for key, candidate := range p.entries {
				if candidate.refs == 0 && (oldest == nil || candidate.used.Before(oldest.used)) {
					oldestKey, oldest = key, candidate
				}
			}
			if oldest == nil {
				p.mu.Unlock()
				return errRuntimeBusy
			}
			if oldest.sdk != nil {
				oldest.sdk.Close()
			}
			delete(p.entries, oldestKey)
		}
		entry = &runtimeEntry{}
		p.entries[scope] = entry
	}
	entry.refs++
	entry.used = time.Now()
	p.mu.Unlock()
	entry.mu.Lock()
	defer func() { entry.mu.Unlock(); p.mu.Lock(); entry.refs--; entry.used = time.Now(); p.mu.Unlock() }()
	hash := sha256.Sum256([]byte(identity.IdentityToken))
	if entry.sdk != nil && (entry.assertionHash != hash) {
		entry.sdk.Close()
		entry.sdk = nil
	}
	if entry.sdk == nil {
		cfg := p.template
		cfg.LicenseKey = ""
		dir := filepath.Join(p.root, scope)
		if err := launchclient.ProtectDirectory(dir); err != nil {
			return err
		}
		cfg.CachePath = filepath.Join(dir, "lease.json")
		sdk, err := launchclient.NewRuntime(cfg, launchclient.RuntimeConfig{Identity: identity, Environment: identity.Environment, CachePath: cfg.CachePath})
		if err != nil {
			return err
		}
		// The pool context outlives any HTTP request. Canceling one request must
		// never disable another request's authorization heartbeat.
		if err = sdk.Start(p.ctx, nil); err != nil {
			sdk.Close()
			return err
		}
		entry.sdk, entry.assertionHash = sdk, hash
	}
	return action(entry.sdk)
}
func (p *runtimePool) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, entry := range p.entries {
		entry.mu.Lock()
		if entry.sdk != nil {
			entry.sdk.Close()
			entry.sdk = nil
		}
		entry.mu.Unlock()
	}
	p.entries = make(map[string]*runtimeEntry)
}

func writeFailure(w http.ResponseWriter, err error) {
	status, code, message := http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Authorization service unavailable; retry later"
	var api *launchclient.APIError
	var sdk *launchclient.RuntimeAPIError
	switch {
	case errors.Is(err, errFeatureDenied):
		status, code, message = http.StatusForbidden, "FEATURE_DENIED", "The SDK denied this feature"
	case errors.Is(err, context.Canceled):
		status, code, message = http.StatusRequestTimeout, "REQUEST_CANCELED", "Request canceled"
	case errors.Is(err, context.DeadlineExceeded):
		status, code, message = http.StatusGatewayTimeout, "SERVICE_TIMEOUT", "Authorization service timed out"
	case errors.As(err, &api):
		code = api.Code
		switch {
		case api.Status == 401:
			status, message = 401, "Sign in again"
		case api.Code == "ENTITLEMENT_PENDING":
			status, message = 409, "Payment received; authorization is being synchronized"
		case api.Code == "ENTITLEMENT_EXPIRED":
			status, message = 403, "Authorization expired; renew your purchase"
		case api.Code == "ENTITLEMENT_SUSPENDED":
			status, message = 403, "Authorization suspended; contact the merchant"
		case api.Code == "ENTITLEMENT_NOT_PURCHASED":
			status, message = 403, "This account has not purchased the product"
		case api.Status == 403:
			status, message = 403, "Authorization denied"
		case api.Status == 429:
			status, message = 429, "Too many requests; retry later"
		}
		if api.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int((api.RetryAfter+time.Second-1)/time.Second)))
		}
	case errors.As(err, &sdk):
		if !sdk.Retryable && (sdk.Status == 400 || sdk.Status == 401 || sdk.Status == 403 || sdk.Status == 409) {
			status, code, message = 403, sdk.Code, "The SDK denied this device or feature"
		}
		if sdk.RetryAfterSeconds > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(sdk.RetryAfterSeconds))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func main() {
	origin, product := os.Getenv("MERCHANT_ORIGIN"), os.Getenv("LAUNCH_PRODUCT_ID")
	environment := os.Getenv("LAUNCH_EXAMPLE_ENV")
	if environment == "" {
		environment = "live"
	}
	if environment != "sandbox" && environment != "live" {
		panic("LAUNCH_EXAMPLE_ENV must be sandbox or live")
	}
	merchant, err := launchclient.New(origin, environment == "sandbox")
	if err != nil {
		panic(err)
	}
	public, err := os.ReadFile(os.Getenv("PRODUCT_CONFIG"))
	if err != nil {
		panic("set PRODUCT_CONFIG to the public product.json")
	}
	var template launchclient.ProductConfig
	if json.Unmarshal(public, &template) != nil {
		panic("invalid public product config")
	}
	if environment == "live" {
		template.AllowHTTP = false
	}
	root, err := os.UserCacheDir()
	if err != nil {
		panic("private cache unavailable")
	}
	lifetime, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	pool := &runtimePool{entries: make(map[string]*runtimeEntry), template: template, root: filepath.Join(root, "LaunchWebExample"), ctx: lifetime}
	defer pool.close()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") || len(authorization) <= 7 || len(authorization) > 256 {
			writeFailure(w, &launchclient.APIError{Code: "UNAUTHENTICATED", Status: 401})
			return
		}
		ctx, done := context.WithTimeout(r.Context(), 15*time.Second)
		defer done()
		customer := merchant.WithToken(strings.TrimPrefix(authorization, "Bearer "))
		user, err := customer.CurrentUser(ctx)
		if err != nil {
			writeFailure(w, err)
			return
		}
		if user.ID == "" {
			writeFailure(w, launchclient.ErrInvalidResponse)
			return
		}
		identity, err := customer.Identity(ctx, product, environment)
		if err != nil {
			writeFailure(w, err)
			return
		}
		// Identity comes from merchant authentication, never a user_id in the body.
		scope := sha256.Sum256([]byte(strings.TrimRight(origin, "/") + "\n" + user.ID + "\n" + product + "\n" + environment + "\n" + template.ProductID + "\n" + authorization))
		var report string
		err = pool.use(hex.EncodeToString(scope[:]), identity, func(sdk launchclient.Runtime) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			// Run business work before writing headers, so an SDK failure cannot turn
			// a partially written 200 response into a misleading success.
			if err := sdk.RunFeature("export", func() error { report = "Protected report\n"; return nil }); err != nil {
				return errFeatureDenied
			}
			return nil
		})
		if err != nil {
			writeFailure(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprint(w, report)
	})
	server := &http.Server{Addr: "127.0.0.1:8090", Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		<-lifetime.Done()
		shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Println("Example backend at http://127.0.0.1:8090; POST /export with the caller's Launch desktop bearer token")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic("example HTTP server stopped")
	}
}
