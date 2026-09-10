package lifecycle

import (
	"context"
	"log/slog"
	"maps"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/clock"
	"github.com/anasatwork01/cofound/packages/chassis/logkey"
)

// Server is the subset of http.Server the sequence drives. An interface so the
// whole sequence is unit-testable with no listener.
type Server interface {
	Shutdown(ctx context.Context) error
	Close() error
}

// Readiness is satisfied structurally by *health.Registry.
type Readiness interface{ SetDraining() }

// Phase names a shutdown step.
type Phase string

const (
	PhaseDeregister Phase = "deregister"
	PhaseDrain      Phase = "drain"
	PhaseShutdown   Phase = "shutdown"
	PhaseClose      Phase = "close"
	PhaseTelemetry  Phase = "telemetry"
)

// Step records what one phase did.
type Step struct {
	Phase    Phase
	Duration time.Duration
	Err      error
	Forced   bool
}

// Report is the outcome, logged as one line.
type Report struct {
	Steps     []Step
	Clean     bool
	Total     time.Duration
	AtStart   map[string]int
	Remaining map[string]int
}

// Step returns the recorded step for p.
func (r Report) Step(p Phase) (Step, bool) {
	for _, s := range r.Steps {
		if s.Phase == p {
			return s, true
		}
	}
	return Step{}, false
}

// Attrs renders the report for a single log line.
func (r Report) Attrs() []slog.Attr {
	phases := make([]any, 0, len(r.Steps))
	for _, s := range r.Steps {
		kv := []any{slog.Int64("ms", s.Duration.Milliseconds())}
		if s.Err != nil {
			kv = append(kv, slog.String(logkey.Err, s.Err.Error()))
		}
		if s.Forced {
			kv = append(kv, slog.Bool("forced", true))
		}
		phases = append(phases, slog.Group(string(s.Phase), kv...))
	}
	out := []slog.Attr{
		slog.Bool(logkey.Clean, r.Clean),
		slog.Int64(logkey.DurationMS, r.Total.Milliseconds()),
		slog.Group(logkey.Phase, phases...),
	}
	if n := total(r.AtStart); n > 0 {
		out = append(out, slog.Int(logkey.StreamsOpen, n))
	}
	if n := total(r.Remaining); n > 0 {
		out = append(out, slog.Int(logkey.StreamsRemaining, n))
	}
	return out
}

func total(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// Sequence is the four-phase shutdown.
//
// Bounding it is not optional: if the drain outlives the orchestrator's
// termination grace period the process is SIGKILLed mid-write, which is exactly
// the unclean cut that Last-Event-ID resume exists to make unnecessary. The
// config layer enforces that the phases sum to less than the budget.
type Sequence struct {
	Server    Server
	Drain     *Drain
	Readiness Readiness
	Clock     clock.Clock
	Log       *slog.Logger
	Telemetry func(context.Context) error

	DeregisterDelay  time.Duration
	DrainTimeout     time.Duration
	ServerTimeout    time.Duration
	TelemetryTimeout time.Duration
}

// Run executes the sequence and returns what happened.
//
// ctx is NOT used as the parent for the phase deadlines: it is already
// cancelled by the time Run is called (that is what triggered shutdown), so a
// derived context would expire instantly and every phase would be skipped.
func (s *Sequence) Run(ctx context.Context) Report {
	clk := s.Clock
	if clk == nil {
		clk = clock.Real()
	}
	start := clk.Now()
	rep := Report{Clean: true}

	if s.Drain != nil {
		rep.AtStart = maps.Clone(s.Drain.Open())
	}

	// Phase 0 — deregister. The listener stays OPEN while the load balancer
	// notices. After Shutdown closes it the port refuses connections outright,
	// which no load balancer can drain: measured as /readyz 200, then
	// /readyz 503 with the port still accepting, then connection refused.
	// Skipping this produces user-visible TCP refusals on every deploy even
	// when the later phases are perfect.
	t0 := clk.Now()
	if s.Readiness != nil {
		s.Readiness.SetDraining()
	}
	if s.DeregisterDelay > 0 {
		<-clk.After(s.DeregisterDelay)
	}
	rep.Steps = append(rep.Steps, Step{Phase: PhaseDeregister, Duration: clk.Since(t0)})

	// Phase 1 — drain. Begin cancels every Stream.Context and refuses new
	// streams; Wait blocks until the open ones finish.
	t1 := clk.Now()
	var drainErr error
	if s.Drain != nil {
		s.Drain.Begin()
		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.DrainTimeout)
		drainErr = s.Drain.Wait(dctx)
		cancel()
		rep.Remaining = maps.Clone(s.Drain.Open())
	}
	if drainErr != nil {
		rep.Clean = false
	}
	rep.Steps = append(rep.Steps, Step{Phase: PhaseDrain, Duration: clk.Since(t1), Err: drainErr})

	// Phase 2 — shutdown. Stops accepting and waits for ordinary requests.
	t2 := clk.Now()
	var shutErr error
	if s.Server != nil {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.ServerTimeout)
		shutErr = s.Server.Shutdown(sctx)
		cancel()
	}
	if shutErr != nil {
		rep.Clean = false
	}
	rep.Steps = append(rep.Steps, Step{Phase: PhaseShutdown, Duration: clk.Since(t2), Err: shutErr})

	// Phase 3 — close. Only when Shutdown could not finish.
	if shutErr != nil && s.Server != nil {
		t3 := clk.Now()
		closeErr := s.Server.Close()
		rep.Steps = append(rep.Steps, Step{
			Phase: PhaseClose, Duration: clk.Since(t3), Err: closeErr, Forced: true,
		})
	}

	// Phase 4 — telemetry, LAST, so the drain's own spans still export.
	if s.Telemetry != nil {
		t4 := clk.Now()
		tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.TelemetryTimeout)
		telErr := s.Telemetry(tctx)
		cancel()
		rep.Steps = append(rep.Steps, Step{Phase: PhaseTelemetry, Duration: clk.Since(t4), Err: telErr})
	}

	rep.Total = clk.Since(start)
	if s.Log != nil {
		s.Log.LogAttrs(context.Background(), slog.LevelInfo, "shutdown complete", rep.Attrs()...)
	}
	return rep
}
