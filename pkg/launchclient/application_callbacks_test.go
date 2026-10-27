package launchclient

import (
	"context"
	"errors"
	"testing"
)

func TestFeatureCallbackCanCloseApplication(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	want := errors.New("export completed; application closed")
	if err := f.app.RunFeature("export", func() error {
		f.app.Close()
		return want
	}); !errors.Is(err, want) {
		t.Fatal("business callback result was lost", err)
	}
	if err := f.app.RunFeature("export", func() error {
		t.Fatal("closed application executed protected work")
		return nil
	}); !errors.Is(err, ErrNotStarted) {
		t.Fatal("closed application retained runtime", err)
	}
	if _, err := f.store.Load(context.Background(), f.app.key); err != nil {
		t.Fatal("ordinary close lost remembered login", err)
	}
	if f.runtimes[0].released != 0 || f.runtimes[0].closed != 1 {
		t.Fatal("ordinary close did not stop runtime without releasing device")
	}
}

func TestFeatureCallbackCanLogout(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	if err := f.app.RunFeature("export", func() error {
		return f.app.Logout(context.Background())
	}); err != nil {
		t.Fatal("business callback could not sign out", err)
	}
	if f.runtimes[0].released != 1 || f.runtimes[0].closed != 1 {
		t.Fatal("callback logout did not release and stop runtime")
	}
	if _, err := f.store.Load(context.Background(), f.app.key); !errors.Is(err, ErrNoSession) {
		t.Fatal("callback logout retained account", err)
	}
	if err := f.app.RunFeature("export", func() error {
		t.Fatal("signed-out application executed protected work")
		return nil
	}); !errors.Is(err, ErrNotStarted) {
		t.Fatal("callback logout retained runtime", err)
	}
}

func TestFeatureCallbackCanRunAnotherFeature(t *testing.T) {
	f := newLifecycleFixture(t)
	f.login(t)
	want := errors.New("nested export failed")
	calls := 0
	if err := f.app.RunFeature("export", func() error {
		return f.app.RunFeature("export", func() error {
			calls++
			return want
		})
	}); !errors.Is(err, want) || calls != 1 {
		t.Fatal("nested business callback did not run exactly once", err, calls)
	}
}
