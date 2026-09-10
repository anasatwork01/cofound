package lifecycle_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/clock"
	"github.com/anasatwork01/cofound/packages/chassis/lifecycle"
)

type fakeServer struct {
	mu           sync.Mutex
	events       *[]string
	shutdownErr  error
	closed       bool
	shutdownSeen bool
}

func (f *fakeServer) Shutdown(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shutdownSeen = true
	*f.events = append(*f.events, "shutdown")
	return f.shutdownErr
}

func (f *fakeServer) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	*f.events = append(*f.events, "close")
	return nil
}

type fakeReadiness struct {
	events *[]string
	mu     sync.Mutex
	set    bool
}

func (f *fakeReadiness) SetDraining() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.set = true
	*f.events = append(*f.events, "deregister")
}

// TestDeregisterHappensBeforeShutdown is the phase-0 guarantee.
//
// After Shutdown closes the listener the port refuses connections outright,
// which no load balancer can drain. Flipping readiness first, with the listener
// still open, is what turns a deploy from user-visible TCP refusals into a
// clean handover.
func TestDeregisterHappensBeforeShutdown(t *testing.T) {
	t.Parallel()
	var events []string
	srv := &fakeServer{events: &events}
	ready := &fakeReadiness{events: &events}

	seq := &lifecycle.Sequence{
		Server: srv, Readiness: ready, Drain: lifecycle.NewDrain(),
		Clock: clock.NewFake(time.Unix(0, 0)),
		Telemetry: func(context.Context) error {
			events = append(events, "telemetry")
			return nil
		},
		DrainTimeout: time.Second, ServerTimeout: time.Second, TelemetryTimeout: time.Second,
	}
	rep := seq.Run(context.Background())

	want := []string{"deregister", "shutdown", "telemetry"}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events = %v, want %v", events, want)
		}
	}
	if !rep.Clean {
		t.Error("a shutdown with nothing open should be clean")
	}
	// Telemetry last, so the drain's own spans still export.
	if _, ok := rep.Step(lifecycle.PhaseTelemetry); !ok {
		t.Error("telemetry phase missing")
	}
}

// TestDeregisterDelayWaitsWithoutBlockingForever proves the delay is driven by
// the injected clock, so production can wait 3s while a test waits none.
func TestDeregisterDelayUsesTheInjectedClock(t *testing.T) {
	t.Parallel()
	fake := clock.NewFake(time.Unix(0, 0))
	var events []string
	seq := &lifecycle.Sequence{
		Server: &fakeServer{events: &events}, Drain: lifecycle.NewDrain(), Clock: fake,
		DeregisterDelay: 3 * time.Second,
		DrainTimeout:    time.Second, ServerTimeout: time.Second,
	}

	done := make(chan lifecycle.Report, 1)
	go func() { done <- seq.Run(context.Background()) }()

	// Wait until the sequence is actually parked on the delay before advancing,
	// or the advance would fire past a deadline nobody is watching yet.
	waitFor(t, func() bool { return fake.Waiters() > 0 })
	fake.Advance(3 * time.Second)

	select {
	case rep := <-done:
		step, ok := rep.Step(lifecycle.PhaseDeregister)
		if !ok || step.Duration != 3*time.Second {
			t.Errorf("deregister step = %+v, want 3s", step)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the clock advanced")
	}
}

// TestDrainRefusesANewStreamOnceBegun: a stream started mid-shutdown would be
// cut immediately. Refusing it lets the client reconnect to a healthy replica
// instead of burning a round trip here.
func TestDrainRefusesANewStreamOnceBegun(t *testing.T) {
	t.Parallel()
	d := lifecycle.NewDrain()

	release, ok := d.Enter("session_events")
	if !ok {
		t.Fatal("Enter should succeed before draining")
	}
	if got := d.Open()["session_events"]; got != 1 {
		t.Errorf("Open = %d, want 1", got)
	}

	d.Begin()
	if _, ok := d.Enter("session_events"); ok {
		t.Error("Enter must be refused once draining has begun")
	}
	if !d.IsDraining() {
		t.Error("IsDraining should be true")
	}
	select {
	case <-d.Draining():
	default:
		t.Error("Draining() must be closed after Begin")
	}

	release()
	if len(d.Open()) != 0 {
		t.Errorf("Open = %v, want empty after release", d.Open())
	}
	// Release must be idempotent: a handler may both defer it and call it.
	release()
	if len(d.Open()) != 0 {
		t.Errorf("double release corrupted the count: %v", d.Open())
	}
}

// TestCooperatingStreamDrainsImmediately is the measured happy path: a handler
// selecting on the broadcast finishes at once, so shutdown costs nothing.
func TestCooperatingStreamDrainsImmediately(t *testing.T) {
	t.Parallel()
	d := lifecycle.NewDrain()
	release, _ := d.Enter("session_events")

	go func() {
		<-d.Draining()
		release()
	}()

	var events []string
	seq := &lifecycle.Sequence{
		Server: &fakeServer{events: &events}, Drain: d, Clock: clock.Real(),
		DrainTimeout: 5 * time.Second, ServerTimeout: time.Second,
	}
	rep := seq.Run(context.Background())

	if !rep.Clean {
		t.Errorf("a cooperating stream should drain cleanly: %+v", rep)
	}
	if n := rep.AtStart["session_events"]; n != 1 {
		t.Errorf("AtStart = %v, want one open stream recorded", rep.AtStart)
	}
	if len(rep.Remaining) != 0 {
		t.Errorf("Remaining = %v, want empty", rep.Remaining)
	}
}

// TestUncooperativeStreamIsBoundedThenForced proves the bound exists. Without
// it the process outlives the orchestrator's grace period and is SIGKILLed
// mid-write — the unclean cut Last-Event-ID resume exists to avoid.
func TestUncooperativeStreamIsBoundedThenForced(t *testing.T) {
	t.Parallel()
	d := lifecycle.NewDrain()
	release, _ := d.Enter("stubborn")
	defer release()

	var events []string
	srv := &fakeServer{events: &events, shutdownErr: errors.New("still busy")}
	seq := &lifecycle.Sequence{
		Server: srv, Drain: d, Clock: clock.Real(),
		DrainTimeout: 20 * time.Millisecond, ServerTimeout: 20 * time.Millisecond,
	}
	rep := seq.Run(context.Background())

	if rep.Clean {
		t.Error("Clean must be false when a stream did not finish")
	}
	drain, _ := rep.Step(lifecycle.PhaseDrain)
	if drain.Err == nil {
		t.Error("the drain step should record its timeout")
	}
	if n := rep.Remaining["stubborn"]; n != 1 {
		t.Errorf("Remaining = %v, want the stuck stream recorded", rep.Remaining)
	}
	closeStep, ok := rep.Step(lifecycle.PhaseClose)
	if !ok || !closeStep.Forced {
		t.Errorf("a failed Shutdown must escalate to a forced Close, got %+v", closeStep)
	}
	if !srv.closed {
		t.Error("Close was never called")
	}
}

// TestRunSurvivesAnAlreadyCancelledContext. ctx is cancelled by definition — it
// is what triggered shutdown — so deriving the phase deadlines from it would
// expire them instantly and skip every phase.
func TestRunSurvivesAnAlreadyCancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var events []string
	srv := &fakeServer{events: &events}
	seq := &lifecycle.Sequence{
		Server: srv, Drain: lifecycle.NewDrain(), Clock: clock.Real(),
		DrainTimeout: time.Second, ServerTimeout: time.Second,
	}
	rep := seq.Run(ctx)

	if !srv.shutdownSeen {
		t.Fatal("Shutdown was skipped because the phase deadline inherited a cancelled context")
	}
	if !rep.Clean {
		t.Errorf("expected a clean shutdown, got %+v", rep)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition never became true")
}
