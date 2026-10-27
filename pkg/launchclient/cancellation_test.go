package launchclient

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// A secure store may complete its atomic write even if cancellation races with
// it. Runtime startup must enforce the account operation's cancellation itself.
type completingSessionStore struct{ SessionStore }

func (s completingSessionStore) Save(ctx context.Context, key string, value []byte) error {
	return s.SessionStore.Save(context.WithoutCancel(ctx), key, value)
}

type cancelingRuntime struct {
	Runtime
	cancel context.CancelFunc
}

func (r cancelingRuntime) Start(ctx context.Context, changed func(error)) error {
	if err := r.Runtime.Start(ctx, changed); err != nil {
		return err
	}
	r.cancel()
	return nil
}

func TestCancellationDuringRuntimeStart(t *testing.T) {
	for _, operation := range []string{"login", "restore"} {
		for _, storeCompletes := range []bool{false, true} {
			name := operation + "/store_checks_context"
			if storeCompletes {
				name = operation + "/store_completes_write"
			}
			t.Run(name, func(t *testing.T) {
				f := newLifecycleFixture(t)
				var saved []byte
				if operation == "restore" {
					f.login(t)
					f.app.Close()
					var err error
					saved, err = f.store.Load(context.Background(), f.app.key)
					if err != nil {
						t.Fatal(err)
					}
				}
				if storeCompletes {
					f.app.store = completingSessionStore{f.store}
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				factory := f.app.config.RuntimeFactory
				candidateIndex := len(f.runtimes)
				f.app.config.RuntimeFactory = func(config RuntimeConfig) (Runtime, error) {
					runtime, err := factory(config)
					return cancelingRuntime{runtime, cancel}, err
				}
				var err error
				if operation == "login" {
					_, err = f.app.Login(ctx, "a@example.invalid", "password")
				} else {
					_, err = f.app.Restore(ctx)
				}
				if !errors.Is(err, context.Canceled) {
					t.Fatal("canceled startup was reported as success", err)
				}
				if f.app.runtime != nil || f.runtimes[candidateIndex].closed != 1 {
					t.Fatal("canceled candidate remained active")
				}
				if err := f.app.RunFeature("export", func() error {
					t.Fatal("canceled startup executed protected work")
					return nil
				}); !errors.Is(err, ErrNotStarted) {
					t.Fatal("canceled startup retained local authorization", err)
				}
				f.app.config.RuntimeFactory = factory
				if operation == "restore" {
					retained, err := f.store.Load(context.Background(), f.app.key)
					if err != nil || !bytes.Equal(saved, retained) {
						t.Fatal("canceling restore discarded the remembered login", err)
					}
					if _, err := f.app.Restore(context.Background()); err != nil {
						t.Fatal("restore could not be retried after cancellation", err)
					}
				} else {
					pending, err := f.app.pendingAccounts(context.Background())
					if err != nil || len(pending) != 1 || !pending[0].LogoutPending {
						t.Fatal("canceled login lost account revocation", err)
					}
					if _, err := f.app.Restore(context.Background()); !errors.Is(err, ErrNoSession) {
						t.Fatal("canceled login was restored instead of revoked", err)
					}
				}
			})
		}
	}
}
