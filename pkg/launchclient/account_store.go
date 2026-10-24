package launchclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// The account lock covers the complete read/request/write lifecycle. It is
// independent of CacheDir: applications sharing a vault may use different
// Licentivo cache directories. No licensing files are read or changed here.
func (a *Application) lockAccount(ctx context.Context) (func(), error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	root = filepath.Join(root, "LicentivoLaunch", "account-locks", "go")
	if err = ProtectDirectory(root); err != nil {
		return nil, err
	}
	return lockAccountFile(ctx, filepath.Join(root, hashScope(a.key)+".lock"))
}

type accountLogoutQueue struct {
	Version int                 `json:"version"`
	Records []rememberedSession `json:"records"`
}

func (a *Application) queueKey() string { return a.key + "\naccount-logout-queue-v1" }

// Contains no bearer or licensing data. An immutable, session-specific intent
// also survives a vault outage or a lifecycle-lock timeout.
func (a *Application) intentPath(token string) (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "LicentivoLaunch", "account-intents", "go", hashScope(a.key)+"."+hashScope(token)+".signedout"), nil
}
func (a *Application) rememberIntent(token string) error {
	if token == "" {
		return nil
	}
	path, err := a.intentPath(token)
	if err != nil {
		return err
	}
	if err = ProtectDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err = file.Write([]byte{1}); err != nil {
		return err
	}
	return file.Sync()
}
func (a *Application) hasIntent(token string) (bool, error) {
	path, err := a.intentPath(token)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}
func (a *Application) clearIntent(token string) error {
	if token == "" {
		return nil
	}
	path, err := a.intentPath(token)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (a *Application) pendingAccounts(ctx context.Context) ([]rememberedSession, error) {
	raw, err := a.store.Load(ctx, a.queueKey())
	if errors.Is(err, ErrNoSession) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	var queue accountLogoutQueue
	if len(raw) > 1<<20 || json.Unmarshal(raw, &queue) != nil || queue.Version != 1 || len(queue.Records) > 64 {
		return nil, ErrSessionMismatch
	}
	for _, r := range queue.Records {
		if !r.LogoutPending || !a.valid(r) {
			return nil, ErrSessionMismatch
		}
	}
	return queue.Records, nil
}

func (a *Application) updatePending(ctx context.Context, record rememberedSession, remove bool) error {
	records, err := a.pendingAccounts(ctx)
	if err != nil {
		return err
	}
	kept := make([]rememberedSession, 0, len(records)+1)
	for _, r := range records {
		if r.Session.AccessToken != record.Session.AccessToken {
			kept = append(kept, r)
		}
	}
	if !remove {
		record.Version, record.LogoutPending = 3, true
		record.Identity.IdentityToken = ""
		kept = append(kept, record)
	}
	if len(kept) == 0 {
		return a.store.Delete(ctx, a.queueKey())
	}
	if len(kept) > 64 {
		return errors.New("too many pending account logouts")
	}
	raw, err := json.Marshal(accountLogoutQueue{Version: 1, Records: kept})
	if err != nil {
		return err
	}
	defer clear(raw)
	if len(raw) > 60000 {
		return errors.New("pending account logouts exceed secure-store capacity")
	}
	return a.store.Save(ctx, a.queueKey(), raw)
}

func (a *Application) ownsActive(ctx context.Context, token string) (bool, error) {
	active, err := a.load(ctx)
	if errors.Is(err, ErrNoSession) {
		return false, nil
	}
	return err == nil && token != "" && active.Session.AccessToken == token, err
}

func (a *Application) deleteOwned(ctx context.Context, token string) error {
	owned, err := a.ownsActive(ctx, token)
	if err != nil || !owned {
		return err
	}
	return a.store.Delete(ctx, a.key)
}

func (a *Application) markAccountLogout(ctx context.Context) error {
	_ = a.rememberIntent(a.record.Session.AccessToken)
	queueErr := a.updatePending(ctx, a.record, false)
	owned, err := a.ownsActive(ctx, a.record.Session.AccessToken)
	if err != nil {
		return errors.Join(queueErr, err)
	}
	if !owned {
		return queueErr
	}
	// Preserve v3 compatibility, but only for the exact session still owning
	// the active slot. A late instance must never overwrite a newer login.
	activeErr := a.save(ctx)
	if activeErr == nil {
		return nil
	}
	// An atomic Save failure can leave a valid v2 login behind. Removing only
	// our own active slot prevents that login being restored after a restart.
	deleteErr := a.deleteOwned(ctx, a.record.Session.AccessToken)
	if queueErr == nil {
		return nil
	}
	return errors.Join(ErrAccountLogoutNotPersisted, queueErr, activeErr, deleteErr)
}

func (a *Application) revokeAccount(ctx context.Context, r rememberedSession) error {
	err := a.client.WithToken(r.Session.AccessToken).SignOut(ctx)
	var api *APIError
	if errors.As(err, &api) && api.Status == 401 {
		return nil
	}
	return err
}

func (a *Application) finishAccount(ctx context.Context, r rememberedSession) error {
	err := errors.Join(a.deleteOwned(ctx, r.Session.AccessToken), a.updatePending(ctx, r, true))
	if err != nil {
		return err
	}
	return a.clearIntent(r.Session.AccessToken)
}

// Queue entries for other sessions only retry account revocation. Reopening a
// licensing runtime for them could load the currently signed-in user's cache.
func (a *Application) retryOtherAccounts(ctx context.Context, skip string) error {
	records, err := a.pendingAccounts(context.Background())
	if err != nil {
		return err
	}
	var result error
	for _, r := range records {
		if r.Session.AccessToken == skip {
			continue
		}
		if err = a.revokeAccount(ctx, r); err != nil {
			result = errors.Join(result, ErrAccountLogoutPending, err)
		} else {
			result = errors.Join(result, a.finishAccount(context.Background(), r))
		}
	}
	return result
}
