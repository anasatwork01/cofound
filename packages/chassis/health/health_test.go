package health_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/clock"
	"github.com/anasatwork01/cofound/packages/chassis/health"
)

func newRegistry(clk clock.Clock) *health.Registry {
	return health.New(health.Options{
		Interval: time.Second, Timeout: 500 * time.Millisecond,
		StaleAfter: 10 * time.Second, Clock: clk,
	})
}

// TestStartBlocksUntilEveryProbeHasRun. A process that reports ready before it
// has checked anything gets traffic it cannot serve — the readiness endpoint
// would be answering from an empty snapshot.
func TestStartBlocksUntilEveryProbeHasRun(t *testing.T) {
	t.Parallel()
	reg := newRegistry(clock.Real())
	var checked atomic.Int64
	reg.Register(health.PingFunc("postgres", func(context.Context) error {
		checked.Add(1)
		return nil
	}, health.Critical))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := reg.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if checked.Load() == 0 {
		t.Fatal("Start returned before the first pass completed")
	}
	if snap := reg.Snapshot(); snap.State != health.StateUp {
		t.Errorf("State = %s, want up", snap.State)
	}
}

// TestUnknownCountsAsFailing: a probe that has never reported is not evidence
// of health.
func TestUnknownCountsAsFailing(t *testing.T) {
	t.Parallel()
	reg := newRegistry(clock.Real())
	reg.Register(health.PingFunc("postgres", func(context.Context) error { return nil }, health.Critical))

	snap := reg.Snapshot() // never started
	if snap.State != health.StateDown {
		t.Errorf("State = %s, want down while a critical probe is unknown", snap.State)
	}
	if got := snap.Failing(); len(got) != 1 || got[0] != "postgres" {
		t.Errorf("Failing = %v, want [postgres]", got)
	}
}

// TestStaleResultBecomesUnknown. A wedged poller must not leave a stale 200
// behind: the last known-good answer is the most dangerous thing to serve.
func TestStaleResultBecomesUnknown(t *testing.T) {
	t.Parallel()
	fake := clock.NewFake(time.Unix(1000, 0))
	reg := health.New(health.Options{
		Interval: time.Second, Timeout: 100 * time.Millisecond,
		StaleAfter: 5 * time.Second, Clock: fake,
	})
	reg.Register(health.PingFunc("redis", func(context.Context) error { return nil }, health.Critical))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := reg.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if reg.Snapshot().State != health.StateUp {
		t.Fatal("should be up right after a successful poll")
	}

	// Move past StaleAfter without letting the poller run again.
	fake.Set(fake.Now().Add(30 * time.Second))
	snap := reg.Snapshot()
	if snap.State != health.StateDown {
		t.Errorf("State = %s, want down once the result is stale", snap.State)
	}
	if snap.Results[0].State != health.StateUnknown {
		t.Errorf("probe state = %s, want unknown", snap.Results[0].State)
	}
}

// TestDegradingDoesNotTakeTrafficAway distinguishes "cannot serve" from
// "serves with less". Treating every dependency as critical means one optional
// integration removes the whole fleet from rotation.
func TestDegradingDoesNotTakeTrafficAway(t *testing.T) {
	t.Parallel()
	reg := newRegistry(clock.Real())
	reg.Register(health.PingFunc("postgres", func(context.Context) error { return nil }, health.Critical))
	reg.Register(health.PingFunc("slack", func(context.Context) error {
		return errors.New("workspace unreachable")
	}, health.Degrading))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := reg.Start(ctx); err != nil {
		t.Fatal(err)
	}
	snap := reg.Snapshot()
	if snap.State != health.StateUp {
		t.Errorf("State = %s, want up: only a Degrading probe is down", snap.State)
	}
	// It must still be reported, or nobody learns the integration is broken.
	if got := snap.Failing(); len(got) != 1 || got[0] != "slack" {
		t.Errorf("Failing = %v, want [slack]", got)
	}
}

// TestSetDrainingIsOneWay. A process that has begun shutting down must never
// re-advertise itself, or the load balancer sends it traffic it is about to
// stop serving.
func TestSetDrainingIsOneWay(t *testing.T) {
	t.Parallel()
	reg := newRegistry(clock.Real())
	reg.Register(health.PingFunc("postgres", func(context.Context) error { return nil }, health.Critical))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := reg.Start(ctx); err != nil {
		t.Fatal(err)
	}
	reg.SetDraining()

	snap := reg.Snapshot()
	if snap.State != health.StateDown || !snap.Draining {
		t.Errorf("snapshot = %+v, want down and draining", snap)
	}
	// Even a fresh successful poll must not undo it.
	reg.Check(ctx)
	if reg.Snapshot().State != health.StateDown {
		t.Error("a successful probe re-advertised a draining process")
	}
}

// TestSinceAndConsecutiveAnswerHowLong, which is the first question in an
// incident and should not need a log search.
func TestSinceAndConsecutiveAnswerHowLong(t *testing.T) {
	t.Parallel()
	reg := newRegistry(clock.Real())
	var fail atomic.Bool
	reg.Register(health.PingFunc("postgres", func(context.Context) error {
		if fail.Load() {
			return errors.New("down")
		}
		return nil
	}, health.Critical))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := reg.Start(ctx); err != nil {
		t.Fatal(err)
	}
	reg.Check(ctx)
	up := reg.Snapshot().Results[0]
	if up.Consecutive < 2 {
		t.Errorf("Consecutive = %d, want it accumulating across polls", up.Consecutive)
	}

	fail.Store(true)
	reg.Check(ctx)
	down := reg.Snapshot().Results[0]
	if down.State != health.StateDown {
		t.Fatalf("State = %s, want down", down.State)
	}
	if down.Consecutive != 1 {
		t.Errorf("Consecutive = %d, want 1: the counter resets on a transition", down.Consecutive)
	}
	if !down.Since.After(up.Since) {
		t.Error("Since must move to the transition time")
	}
}

// TestProbeErrorNeverEscapesTheSnapshotNames. /readyz is unauthenticated, so
// the public body may name a failing probe but never carry its error string —
// which routinely contains a hostname, a port or a driver message.
func TestProbeErrorIsSeparateFromPublicNames(t *testing.T) {
	t.Parallel()
	reg := newRegistry(clock.Real())
	reg.Register(health.PingFunc("postgres", func(context.Context) error {
		return errors.New("dial tcp 10.0.3.14:5432: connect: connection refused")
	}, health.Critical))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := reg.Start(ctx); err != nil {
		t.Fatal(err)
	}
	snap := reg.Snapshot()
	for _, name := range snap.Failing() {
		if name != "postgres" {
			t.Errorf("Failing returned %q; it must be names only", name)
		}
	}
	// The detail is still available to the log and the loopback admin listener.
	if snap.Results[0].Err == nil {
		t.Error("the error must be retained for the admin listener and the log")
	}
}

// TestRegisterRejectsMisuse: a probe silently dropped at boot is a dependency
// nobody is watching, so misuse panics rather than degrading.
func TestRegisterRejectsMisuse(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*health.Registry){
		"empty name": func(r *health.Registry) {
			r.Register(health.Probe{Check: func(context.Context) error { return nil }})
		},
		"nil check": func(r *health.Registry) { r.Register(health.Probe{Name: "x"}) },
		"duplicate": func(r *health.Registry) {
			p := health.PingFunc("x", func(context.Context) error { return nil }, health.Critical)
			r.Register(p)
			r.Register(p)
		},
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Error("expected a panic")
				}
			}()
			fn(newRegistry(clock.Real()))
		})
	}
}

// TestProbeTimeoutIsEnforced, so one hung dependency cannot stall the poller
// and make every other probe go stale.
func TestProbeTimeoutIsEnforced(t *testing.T) {
	t.Parallel()
	reg := health.New(health.Options{
		Interval: time.Second, Timeout: 20 * time.Millisecond,
		StaleAfter: 10 * time.Second, Clock: clock.Real(),
	})
	reg.Register(health.PingFunc("hung", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}, health.Critical))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Now()
	if err := reg.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Start took %s; the probe timeout was not enforced", elapsed)
	}
	if reg.Snapshot().State != health.StateDown {
		t.Error("a timed-out probe must count as down")
	}
}
