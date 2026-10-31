package launchclient

import (
	"context"
	"errors"
	"testing"
)

type meterAdapter struct {
	fakeRuntime
	feature, id string
	quantity    int64
	settled     bool
	complete    bool
	calls       int
}

func (r *meterAdapter) RunMeteredFeature(ctx context.Context, f string, q int64, id string, action func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.feature, r.quantity, r.id = f, q, id
	if r.complete {
		return nil
	}
	r.calls++
	if err := action(); err != nil {
		return err
	}
	r.complete = true
	return nil
}
func (r *meterAdapter) ResolveMeteredFeature(_ context.Context, id string, succeeded bool) error {
	r.id = id
	r.settled = succeeded
	return nil
}

func TestApplicationDelegatesMeteredTasksWithoutOrdinaryFallback(t *testing.T) {
	ctx := context.Background()
	a := &Application{}
	if err := a.RunMeteredFeature(ctx, "export", 7, "job-1", nil); !errors.Is(err, ErrNotStarted) {
		t.Fatal(err)
	}
	a.runtime = &fakeRuntime{}
	if err := a.RunMeteredFeature(ctx, "export", 7, "job-1", func() error { t.Fatal("ordinary feature fallback ran business"); return nil }); !errors.Is(err, ErrMeteringUnsupported) {
		t.Fatal(err)
	}
	if err := a.ResolveMeteredFeature(ctx, "job-1", true); !errors.Is(err, ErrMeteringUnsupported) {
		t.Fatal(err)
	}
	r := &meterAdapter{}
	a.runtime = r
	for range 2 {
		if err := a.RunMeteredFeature(ctx, "export", 7, "job-1", func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if r.calls != 1 || r.feature != "export" || r.quantity != 7 || r.id != "job-1" {
		t.Fatal("task routing or stable replay changed")
	}
	if err := a.ResolveMeteredFeature(ctx, "job-1", true); err != nil || !r.settled || r.id != "job-1" {
		t.Fatal("resolve did not reach the same runtime")
	}
	r.complete = false
	want := errors.New("business failed")
	if err := a.RunMeteredFeature(ctx, "export", 1, "job-2", func() error { return want }); !errors.Is(err, want) {
		t.Fatal("business error lost", err)
	}
}

var _ MeteredRuntime = (*officialRuntime)(nil)
