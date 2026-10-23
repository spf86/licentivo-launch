package launchclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrSessionExpired     = errors.New("remembered session expired; sign in again")
	ErrSessionMismatch    = errors.New("remembered session does not match this account or product")
	ErrOfflineUnavailable = errors.New("no valid signed SDK grant available for offline startup")
	ErrNotStarted         = errors.New("application authorization is not started")
)

// Runtime must delegate to the official Licentivo SDK. In particular,
// CanStartOffline must inspect the SDK's cryptographically verified cached grant;
// neither merchant expiry nor any boolean in a session file grants authorization.
type Runtime interface {
	Start(context.Context, func(error)) error
	CanStartOffline() bool
	RunFeature(string, func() error) error
	Deactivate(context.Context) error
	Close()
}

type RuntimeConfig struct {
	Credential  Credential
	CachePath   string
	User        User
	Environment string
}

type Config struct {
	Origin         string
	ProductID      string
	Environment    string
	AllowLocalHTTP bool
	// Nil Store is explicitly memory-only. Remembered login requires a protected store.
	Store          SessionStore
	CacheDir       string
	Product        ProductConfig
	RuntimeFactory func(RuntimeConfig) (Runtime, error)
	RetryPolicy    RetryPolicy
	OnRuntimeError func(error)
}

type State struct {
	User      User
	ExpiresAt time.Time
	// OfflineFallback means Launch could not be reached and the SDK verified a
	// cached grant. It is informational; RunFeature always asks the SDK again.
	OfflineFallback bool
}

type rememberedSession struct {
	Version     int        `json:"version"`
	Origin      string     `json:"origin"`
	ProductID   string     `json:"product_id"`
	Environment string     `json:"environment"`
	Session     Session    `json:"session"`
	Credential  Credential `json:"credential"`
}

// Application serializes login, restoration, protected work and shutdown.
// Use an application-lifetime context for Login/Restore because the official
// SDK uses it for its heartbeat. Close stops that runtime without signing out.
type Application struct {
	mu      sync.Mutex
	config  Config
	client  *Client
	store   SessionStore
	key     string
	record  rememberedSession
	runtime Runtime
}

func NewApplication(config Config) (*Application, error) {
	client, err := New(config.Origin, config.AllowLocalHTTP && config.Environment == "sandbox")
	if err != nil {
		return nil, err
	}
	if config.Environment != "sandbox" && config.Environment != "live" {
		return nil, errors.New("environment must be sandbox or live")
	}
	if !productIDPattern.MatchString(config.ProductID) {
		return nil, errors.New("invalid product identifier")
	}
	if config.RuntimeFactory == nil {
		if !productIDPattern.MatchString(config.Product.ProductID) || len(config.Product.TrustedKeys) == 0 {
			return nil, errors.New("public Product configuration is required")
		}
		config.RuntimeFactory = func(runtime RuntimeConfig) (Runtime, error) { return NewRuntime(config.Product, runtime) }
	}
	if config.CacheDir == "" {
		root, err := os.UserCacheDir()
		if err != nil {
			return nil, errors.New("could not resolve private application cache")
		}
		config.CacheDir = filepath.Join(root, "LicentivoLaunch", "runtime")
	}
	root, err := filepath.Abs(config.CacheDir)
	if err != nil {
		return nil, err
	}
	config.CacheDir = root
	if config.Store == nil {
		config.Store = &MemoryStore{}
	}
	config.Origin = client.base
	return &Application{config: config, client: client, store: config.Store, key: "launch-session-v1\n" + client.base + "\n" + config.ProductID + "\n" + config.Environment + "\n" + config.Product.ProductID}, nil
}

func (a *Application) cachePath(userID string) string {
	return filepath.Join(a.config.CacheDir, hashScope(a.key+"\n"+userID), "lease.json")
}
func (a *Application) valid(record rememberedSession) bool {
	return record.Version == 1 && record.Origin == a.config.Origin && record.ProductID == a.config.ProductID && record.Environment == a.config.Environment && record.Session.User.ID != "" && record.Session.AccessToken != "" && record.Credential.ProductID == a.config.ProductID && record.Credential.LicenseKey != ""
}
func (a *Application) load(ctx context.Context) (rememberedSession, error) {
	raw, err := a.store.Load(ctx, a.key)
	if err != nil {
		return rememberedSession{}, err
	}
	defer clear(raw)
	var record rememberedSession
	if json.Unmarshal(raw, &record) != nil || !a.valid(record) {
		return rememberedSession{}, ErrSessionMismatch
	}
	return record, nil
}
func (a *Application) save(ctx context.Context) error {
	raw, err := json.Marshal(a.record)
	if err != nil {
		return err
	}
	defer clear(raw)
	return a.store.Save(ctx, a.key, raw)
}

func (a *Application) start(ctx context.Context, offline bool) (State, error) {
	path := a.cachePath(a.record.Session.User.ID)
	if err := ProtectDirectory(filepath.Dir(path)); err != nil {
		return State{}, err
	}
	runtime, err := a.config.RuntimeFactory(RuntimeConfig{Credential: a.record.Credential, CachePath: path, User: a.record.Session.User, Environment: a.config.Environment})
	if err != nil {
		return State{}, err
	}
	if offline && !runtime.CanStartOffline() {
		runtime.Close()
		return State{}, ErrOfflineUnavailable
	}
	if err = runtime.Start(ctx, a.config.OnRuntimeError); err != nil {
		runtime.Close()
		return State{}, err
	}
	a.runtime = runtime
	return State{User: a.record.Session.User, ExpiresAt: a.record.Session.ExpiresAt, OfflineFallback: offline}, nil
}

func (a *Application) Login(ctx context.Context, email, password string) (State, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Switching accounts cannot retain a previous account's running runtime.
	if err := a.logoutLocked(ctx); err != nil {
		return State{}, err
	}
	session, err := a.client.LoginSession(ctx, email, password)
	if err != nil {
		return State{}, err
	}
	a.record = rememberedSession{Version: 1, Origin: a.config.Origin, ProductID: a.config.ProductID, Environment: a.config.Environment, Session: session}
	if session.ExpiresAt.IsZero() || !session.ExpiresAt.After(time.Now()) {
		return State{}, errors.Join(ErrSessionExpired, a.logoutLocked(ctx))
	}
	credential, err := a.client.CredentialWithRetry(ctx, a.config.ProductID, a.config.Environment, a.config.RetryPolicy)
	if err != nil {
		return State{}, errors.Join(err, a.logoutLocked(ctx))
	}
	a.record.Credential = credential
	state, err := a.start(ctx, false)
	if err != nil {
		return State{}, errors.Join(err, a.logoutLocked(ctx))
	}
	if err = a.save(ctx); err != nil {
		return State{}, errors.Join(err, a.logoutLocked(ctx))
	}
	return state, nil
}

func (a *Application) Restore(ctx context.Context) (State, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runtime != nil {
		a.runtime.Close()
		a.runtime = nil
	}
	record, err := a.load(ctx)
	if err != nil {
		return State{}, err
	}
	a.record = record
	a.client = a.client.WithToken(record.Session.AccessToken)
	if record.Session.ExpiresAt.IsZero() || !record.Session.ExpiresAt.After(time.Now()) {
		return State{}, errors.Join(ErrSessionExpired, a.clearLocked())
	}
	user, err := a.client.CurrentUser(ctx)
	if err != nil {
		if ServiceUnavailable(err) {
			return a.start(ctx, true)
		}
		if authenticationDenied(err) {
			return State{}, errors.Join(err, a.clearLocked())
		}
		return State{}, err
	}
	if user.ID != record.Session.User.ID {
		return State{}, errors.Join(ErrSessionMismatch, a.clearLocked())
	}
	credential, err := a.client.CredentialWithRetry(ctx, a.config.ProductID, a.config.Environment, a.config.RetryPolicy)
	if err != nil {
		if ServiceUnavailable(err) {
			return a.start(ctx, true)
		}
		if authenticationDenied(err) {
			return State{}, errors.Join(err, a.clearLocked())
		}
		return State{}, err
	}
	a.record.Session.User = user
	a.record.Credential = credential
	state, err := a.start(ctx, false)
	if err != nil {
		return State{}, err
	}
	if err = a.save(ctx); err != nil {
		a.runtime.Close()
		a.runtime = nil
		return State{}, err
	}
	return state, nil
}

func authenticationDenied(err error) bool {
	var api *APIError
	return errors.As(err, &api) && api.Code != "ENTITLEMENT_PENDING" && (api.Status == 401 || api.Status == 403 || api.Code == "ENTITLEMENT_NOT_PURCHASED")
}

// RunFeature performs no merchant-metadata checks. Every invocation is decided
// by the SDK. The action must not reenter this Application's lifecycle methods.
func (a *Application) RunFeature(feature string, action func() error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runtime == nil {
		return ErrNotStarted
	}
	return a.runtime.RunFeature(feature, action)
}

// Close stops the SDK and returns floating seats according to SDK policy. It
// keeps the protected Launch session for the next startup.
func (a *Application) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runtime != nil {
		a.runtime.Close()
		a.runtime = nil
	}
	a.client.token = ""
}

func (a *Application) clearLocked() error {
	if a.runtime != nil {
		a.runtime.Close()
		a.runtime = nil
	}
	var cacheErr error
	if a.record.Session.User.ID != "" {
		// The path is always our generated hash directory inside CacheDir;
		// no saved or user-controlled path is trusted for recursive removal.
		cacheErr = os.RemoveAll(filepath.Dir(a.cachePath(a.record.Session.User.ID)))
	}
	a.record = rememberedSession{}
	a.client.token = ""
	// Local logout must finish even when the caller's network context expired.
	return errors.Join(cacheErr, a.store.Delete(context.Background(), a.key))
}

func (a *Application) logoutLocked(ctx context.Context) error {
	if a.record.Session.AccessToken == "" {
		record, err := a.load(context.Background())
		if err == nil {
			a.record = record
		} else if !errors.Is(err, ErrNoSession) {
			return errors.Join(err, a.clearLocked())
		}
	}
	var remoteErr error
	if a.record.Session.AccessToken != "" {
		remoteErr = a.client.WithToken(a.record.Session.AccessToken).SignOut(ctx)
		var api *APIError
		if errors.As(remoteErr, &api) && api.Status == 401 {
			remoteErr = nil
		}
	}
	return errors.Join(remoteErr, a.clearLocked())
}

// Logout revokes the merchant session and clears all local account/runtime
// state even if remote revocation fails. It does not release node-locked devices.
func (a *Application) Logout(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.logoutLocked(ctx)
}

// ReleaseDevice explicitly asks the SDK to deactivate this device, then clears
// local state. A remote failure is returned; callers must not report release as
// successful when the network request failed.
func (a *Application) ReleaseDevice(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runtime == nil {
		return ErrNotStarted
	}
	err := a.runtime.Deactivate(ctx)
	return errors.Join(err, a.logoutLocked(ctx))
}
