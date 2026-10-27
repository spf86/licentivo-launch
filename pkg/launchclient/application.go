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
	ErrSessionExpired            = errors.New("remembered account session expired; sign in again")
	ErrSessionMismatch           = errors.New("remembered account does not match this merchant or product")
	ErrOfflineUnavailable        = errors.New("Launch does not decide offline authorization; use the official Licentivo SDK")
	ErrNotStarted                = errors.New("Licentivo runtime has not been started")
	ErrRevocationPending         = errors.New("signed out locally; Licentivo device release is pending and retained by its SDK")
	ErrAccountLogoutPending      = errors.New("signed out locally; Launch account logout is pending")
	ErrAccountLogoutNotPersisted = errors.New("account logout retry could not be persisted; retry before closing this process")
)

// Runtime is a thin delegation boundary to the official SDK. Launch never
// examines a signed grant, device allowance, expiry, feature, or offline cache.
type Runtime interface {
	Start(context.Context, func(error)) error
	RunFeature(string, func() error) error
	Deactivate(context.Context) error
	Close()
}
type RuntimeConfig struct {
	Identity    Identity
	CachePath   string
	User        User
	Environment string
}
type Config struct {
	Origin         string
	ProductID      string
	Environment    string
	AllowLocalHTTP bool
	AllowHTTP      bool
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
}
type rememberedSession struct {
	Version     int     `json:"version"`
	Origin      string  `json:"origin"`
	ProductID   string  `json:"product_id"`
	Environment string  `json:"environment"`
	Session     Session `json:"session"`
	// Only public routing context is retained; IdentityToken is always empty.
	Identity Identity `json:"identity_context"`
	// A pending record is an account revocation task, never a restorable login.
	LogoutPending bool `json:"logout_pending,omitempty"`
}
type Application struct {
	mu             sync.Mutex
	config         Config
	client         *Client
	store          SessionStore
	key            string
	record         rememberedSession
	runtime        Runtime
	accountCleared bool
}

func NewApplication(config Config) (*Application, error) {
	if config.AllowHTTP && config.Environment != "sandbox" {
		return nil, errors.New("HTTP opt-in is restricted to sandbox")
	}
	client, e := newClient(config.Origin, config.AllowLocalHTTP && config.Environment == "sandbox", config.AllowHTTP)
	if e != nil {
		return nil, e
	}
	if config.Environment != "live" && config.Environment != "sandbox" || !productIDPattern.MatchString(config.ProductID) {
		return nil, errors.New("invalid account configuration")
	}
	if config.RuntimeFactory == nil {
		if !productIDPattern.MatchString(config.Product.ProductID) || len(config.Product.TrustedKeys) == 0 {
			return nil, errors.New("public product configuration is required")
		}
		config.RuntimeFactory = func(r RuntimeConfig) (Runtime, error) { return NewRuntime(config.Product, r) }
	}
	if config.CacheDir == "" {
		root, e := os.UserCacheDir()
		if e != nil {
			return nil, e
		}
		config.CacheDir = filepath.Join(root, "LicentivoLaunch", "runtime")
	}
	root, e := filepath.Abs(config.CacheDir)
	if e != nil {
		return nil, e
	}
	config.CacheDir = root
	if config.Store == nil {
		config.Store = &MemoryStore{}
	}
	config.Origin = client.base
	return &Application{config: config, client: client, store: config.Store, key: "launch-account-v2\n" + client.base + "\n" + config.ProductID + "\n" + config.Environment + "\n" + config.Product.ProductID}, nil
}
func (a *Application) cachePath(user string) string {
	return filepath.Join(a.config.CacheDir, hashScope(a.key+"\n"+user), "lease.json")
}
func (a *Application) valid(r rememberedSession) bool {
	if (r.Version != 2 && !(r.Version == 3 && r.LogoutPending)) || r.Origin != a.config.Origin || r.ProductID != a.config.ProductID || r.Environment != a.config.Environment || r.Session.AccessToken == "" || r.Identity.IdentityToken != "" {
		return false
	}
	return r.LogoutPending || r.Session.User.ID != "" && r.Identity.ProductID == a.config.ProductID && r.Identity.Environment == a.config.Environment && productIDPattern.MatchString(r.Identity.LaunchConnectionID) && productIDPattern.MatchString(r.Identity.LicentivoProductID)
}
func (a *Application) load(ctx context.Context) (rememberedSession, error) {
	raw, e := a.store.Load(ctx, a.key)
	if e != nil {
		return rememberedSession{}, e
	}
	defer clear(raw)
	var r rememberedSession
	if json.Unmarshal(raw, &r) != nil || !a.valid(r) {
		return r, ErrSessionMismatch
	}
	return r, nil
}
func (a *Application) save(ctx context.Context) error {
	r := a.record
	r.Identity.IdentityToken = ""
	raw, e := json.Marshal(r)
	if e != nil {
		return e
	}
	defer clear(raw)
	return a.store.Save(ctx, a.key, raw)
}
func (a *Application) makeRuntime(identity Identity) (Runtime, error) {
	path := a.cachePath(a.record.Session.User.ID)
	if e := ProtectDirectory(filepath.Dir(path)); e != nil {
		return nil, e
	}
	return a.config.RuntimeFactory(RuntimeConfig{Identity: identity, CachePath: path, User: a.record.Session.User, Environment: a.config.Environment})
}
func (a *Application) start(ctx context.Context, identity Identity) (State, error) {
	if e := ctx.Err(); e != nil {
		return State{}, e
	}
	r, e := a.makeRuntime(identity)
	if e != nil {
		return State{}, e
	}
	if e = ctx.Err(); e == nil {
		e = r.Start(ctx, a.config.OnRuntimeError)
	}
	if e == nil {
		e = ctx.Err()
	}
	if e != nil {
		r.Close()
		return State{}, e
	}
	a.runtime = r
	return State{a.record.Session.User, a.record.Session.ExpiresAt}, nil
}
func (a *Application) Login(ctx context.Context, email, password string) (State, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	unlock, e := a.lockAccount(ctx)
	if e != nil {
		return State{}, e
	}
	defer unlock()
	if e := a.logoutLocked(ctx); e != nil {
		return State{}, e
	}
	session, e := a.client.LoginSession(ctx, email, password)
	if e != nil {
		return State{}, e
	}
	a.record = rememberedSession{Version: 2, Origin: a.config.Origin, ProductID: a.config.ProductID, Environment: a.config.Environment, Session: session}
	a.accountCleared = false
	if !session.ExpiresAt.After(time.Now()) {
		return State{}, errors.Join(ErrSessionExpired, a.logoutLocked(ctx))
	}
	identity, e := a.client.Identity(ctx, a.config.ProductID, a.config.Environment)
	if e != nil {
		return State{}, errors.Join(e, a.logoutLocked(ctx))
	}
	a.record.Identity = identity
	a.record.Identity.IdentityToken = ""
	state, e := a.start(ctx, identity)
	if e != nil {
		return State{}, errors.Join(e, a.logoutLocked(ctx))
	}
	if e = a.save(ctx); e != nil {
		return State{}, errors.Join(e, a.logoutLocked(ctx))
	}
	return state, nil
}

// Restore authenticates the Launch account online. Authorization restoration
// and offline startup, when needed, belong to the official SDK directly.
func (a *Application) Restore(ctx context.Context) (State, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	unlock, lockErr := a.lockAccount(ctx)
	if lockErr != nil {
		return State{}, lockErr
	}
	defer unlock()
	if a.runtime != nil {
		a.runtime.Close()
		a.runtime = nil
	}
	r := a.record
	if !r.LogoutPending {
		var e error
		r, e = a.load(ctx)
		if e != nil && !errors.Is(e, ErrNoSession) {
			return State{}, e
		}
		pending, pendingErr := a.pendingAccounts(ctx)
		if pendingErr != nil {
			return State{}, pendingErr
		}
		for _, task := range pending {
			if r.Session.AccessToken == "" || task.Session.AccessToken == r.Session.AccessToken {
				r = task
				break
			}
		}
		if r.Session.AccessToken == "" {
			return State{}, ErrNoSession
		}
		intent, intentErr := a.hasIntent(r.Session.AccessToken)
		if intentErr != nil {
			return State{}, intentErr
		}
		if intent {
			r.Version, r.LogoutPending = 3, true
		}
	}
	a.record = r
	a.accountCleared = false
	if r.LogoutPending {
		if e := a.logoutLocked(ctx); e != nil {
			return State{}, e
		}
		return State{}, ErrNoSession
	}
	if e := a.retryOtherAccounts(ctx, r.Session.AccessToken); e != nil {
		return State{}, e
	}
	a.client = a.client.WithToken(r.Session.AccessToken)
	if !r.Session.ExpiresAt.After(time.Now()) {
		return State{}, errors.Join(ErrSessionExpired, a.clearLocked())
	}
	user, e := a.client.CurrentUser(ctx)
	if e != nil {
		if authenticationDenied(e) {
			return State{}, errors.Join(e, a.clearLocked())
		}
		return State{}, e
	}
	if user.ID != r.Session.User.ID {
		return State{}, errors.Join(ErrSessionMismatch, a.clearLocked())
	}
	identity, e := a.client.Identity(ctx, a.config.ProductID, a.config.Environment)
	if e != nil {
		return State{}, e
	}
	a.record.Session.User = user
	a.record.Identity = identity
	a.record.Identity.IdentityToken = ""
	state, e := a.start(ctx, identity)
	if e != nil {
		return State{}, e
	}
	if e = a.save(ctx); e != nil {
		return State{}, errors.Join(e, a.clearLocked())
	}
	return state, nil
}
func authenticationDenied(e error) bool {
	var api *APIError
	return errors.As(e, &api) && (api.Status == 401 || api.Status == 403 && api.Code != "SDK_HTTP_DISABLED" && api.Code != "SDK_HTTPS_REQUIRED")
}
func (a *Application) RunFeature(feature string, action func() error) error {
	a.mu.Lock()
	runtime := a.runtime
	a.mu.Unlock()
	if runtime == nil {
		return ErrNotStarted
	}
	// Business actions may call back into the account lifecycle, including Logout
	// or Close. The runtime owns authorization and synchronization of its state.
	return runtime.RunFeature(feature, action)
}

// Close is ordinary shutdown. Only the official SDK owns its persistent files.
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
	token := a.record.Session.AccessToken
	a.record = rememberedSession{}
	a.accountCleared = true
	a.client.token = ""
	return a.deleteOwned(context.Background(), token)
}
func (a *Application) logoutLocked(ctx context.Context) error {
	if a.record.Session.AccessToken == "" {
		r, e := a.load(context.Background())
		if e == nil {
			a.record = r
		} else if !errors.Is(e, ErrNoSession) {
			return errors.Join(e, a.clearLocked())
		}
	}
	if a.record.Session.AccessToken == "" {
		pending, err := a.pendingAccounts(context.Background())
		if err != nil {
			return errors.Join(err, a.clearLocked())
		}
		if len(pending) > 0 {
			a.record = pending[0]
		}
	}
	otherErr := a.retryOtherAccounts(ctx, a.record.Session.AccessToken)
	var releaseErr, accountErr error
	var persistErr error
	if a.record.Session.AccessToken != "" {
		// Persist intent before either remote request. Cancellation must not turn
		// an interrupted logout back into a remembered login after a restart.
		a.record.LogoutPending = true
		a.record.Version = 3
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		persistErr = a.markAccountLogout(persist)
		cancel()
	}
	// Reopen the official SDK after a process restart so it can deny local work
	// and preserve/retry its unlink-only ticket before the Launch store is cleared.
	active, activeErr := a.load(context.Background())
	canReopen := errors.Is(activeErr, ErrNoSession) || activeErr == nil && active.Session.AccessToken == a.record.Session.AccessToken
	if a.runtime == nil && canReopen && a.record.Session.User.ID != "" && a.record.Identity.LaunchConnectionID != "" {
		a.runtime, releaseErr = a.makeRuntime(a.record.Identity)
	}
	if a.runtime != nil {
		releaseErr = a.runtime.Deactivate(ctx)
	}
	if a.record.Session.AccessToken != "" {
		accountErr = a.revokeAccount(ctx, a.record)
	}
	if accountErr == nil {
		// A confirmed server revocation makes the retry record unnecessary,
		// even if its earlier save failed.
		finished := a.finishAccount(context.Background(), a.record)
		return errors.Join(otherErr, releaseErr, finished, a.clearLocked())
	}
	if a.runtime != nil {
		a.runtime.Close()
		a.runtime = nil
	}
	a.client.token = ""
	return errors.Join(otherErr, ErrAccountLogoutPending, accountErr, persistErr, releaseErr)
}

// Logout stops local use and retains a pending account task until Launch
// confirms revocation. Login, Restore and subsequent Logout retry that task.
// The official SDK's .logout.json is never removed by Launch, even offline.
func (a *Application) Logout(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = a.rememberIntent(a.record.Session.AccessToken)
	lockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	unlock, err := a.lockAccount(lockCtx)
	if err != nil {
		var releaseErr error
		if a.runtime != nil {
			releaseErr = a.runtime.Deactivate(ctx)
			a.runtime.Close()
			a.runtime = nil
		}
		a.record.LogoutPending = true
		a.record.Version = 3
		a.client.token = ""
		return errors.Join(ErrAccountLogoutPending, ErrAccountLogoutNotPersisted, err, releaseErr)
	}
	defer unlock()
	if a.accountCleared && a.record.Session.AccessToken == "" {
		return a.retryOtherAccounts(ctx, "")
	}
	return a.logoutLocked(ctx)
}
func (a *Application) ReleaseDevice(ctx context.Context) error { return a.Logout(ctx) }
