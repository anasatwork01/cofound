// Package health is a background-polled readiness registry.
//
// /readyz reads an in-memory snapshot and never touches the database. A
// per-request fan-out turns a load balancer's probe rate into database load and
// amplifies a brief blip into a synchronised, cluster-wide outage — every
// replica failing its probe at once because they all queried a struggling
// primary simultaneously.
package health

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/clock"
	"github.com/anasatwork01/cofound/packages/chassis/logkey"
)

// State is a probe's or the process's readiness.
type State string

const (
	StateUp      State = "up"
	StateDown    State = "down"
	StateUnknown State = "unknown"
)

// Criticality says whether a failure should stop traffic.
type Criticality int

const (
	// Critical means the service cannot serve without it.
	Critical Criticality = iota
	// Degrading means the service still serves, with reduced function.
	Degrading
)

func (c Criticality) String() string {
	if c == Degrading {
		return "degrading"
	}
	return "critical"
}

// Probe is one dependency check.
type Probe struct {
	Name        string
	Criticality Criticality
	// Check must respect ctx and must NOT retry internally: the registry's
	// interval is the retry, and an internal retry hides latency from the
	// measurement while extending it past the timeout.
	Check func(context.Context) error
}

// Result is the latest outcome for one probe.
//
// Since and Consecutive exist because the first question in an incident is
// "how long has this been down", and that should be answerable from the
// readiness output without a log search.
type Result struct {
	Name        string
	Criticality Criticality
	State       State
	Latency     time.Duration
	CheckedAt   time.Time
	Since       time.Time
	Consecutive int
	// Err reaches the log and the loopback admin listener, NEVER the
	// unauthenticated main-port /readyz body.
	Err error
}

// Options configures the registry.
type Options struct {
	Interval   time.Duration
	Timeout    time.Duration
	StaleAfter time.Duration
	Log        *slog.Logger
	Clock      clock.Clock
}

// Registry polls probes and serves a snapshot.
type Registry struct {
	opts    Options
	clk     clock.Clock
	log     *slog.Logger
	started bool

	mu       sync.RWMutex
	probes   []Probe
	results  map[string]Result
	draining bool
}

// New returns a registry with sane defaults.
func New(o Options) *Registry {
	if o.Interval <= 0 {
		o.Interval = 2 * time.Second
	}
	if o.Timeout <= 0 {
		o.Timeout = time.Second
	}
	if o.StaleAfter <= 0 {
		o.StaleAfter = 10 * time.Second
	}
	clk := o.Clock
	if clk == nil {
		clk = clock.Real()
	}
	return &Registry{opts: o, clk: clk, log: o.Log, results: map[string]Result{}}
}

// Register adds a probe. It panics on misuse: an unnamed, duplicated or
// late-registered probe is a programming error, and a probe silently dropped at
// boot is a dependency nobody is watching.
func (r *Registry) Register(p Probe) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		panic("health: Register after Start")
	}
	if p.Name == "" {
		panic("health: probe needs a name")
	}
	if p.Check == nil {
		panic("health: probe " + p.Name + " needs a Check")
	}
	for _, e := range r.probes {
		if e.Name == p.Name {
			panic("health: duplicate probe " + p.Name)
		}
	}
	r.probes = append(r.probes, p)
}

// Start polls until ctx is cancelled.
//
// It returns once every probe has been checked at least once, so the process
// never reports ready before it knows. Transitions are logged; individual polls
// are not — a line per poll per probe is noise that hides the transition.
func (r *Registry) Start(ctx context.Context) error {
	r.mu.Lock()
	r.started = true
	probes := append([]Probe(nil), r.probes...)
	r.mu.Unlock()

	if len(probes) == 0 {
		return nil
	}

	first := make(chan struct{})
	go func() {
		r.pollAll(ctx, probes)
		close(first)

		t := r.clk.NewTicker(r.opts.Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C():
				r.pollAll(ctx, probes)
			}
		}
	}()

	select {
	case <-first:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Registry) pollAll(ctx context.Context, probes []Probe) {
	var wg sync.WaitGroup
	for _, p := range probes {
		wg.Add(1)
		go func(p Probe) {
			defer wg.Done()
			r.poll(ctx, p)
		}(p)
	}
	wg.Wait()
}

func (r *Registry) poll(ctx context.Context, p Probe) {
	cctx, cancel := context.WithTimeout(ctx, r.opts.Timeout)
	defer cancel()

	start := r.clk.Now()
	err := p.Check(cctx)
	latency := r.clk.Since(start)

	state := StateUp
	if err != nil {
		state = StateDown
	}

	r.mu.Lock()
	prev := r.results[p.Name]
	now := r.clk.Now()
	res := Result{
		Name:        p.Name,
		Criticality: p.Criticality,
		State:       state,
		Latency:     latency,
		CheckedAt:   now,
		Err:         err,
		Since:       prev.Since,
		Consecutive: prev.Consecutive + 1,
	}
	transitioned := prev.State != state
	if transitioned || prev.Since.IsZero() {
		res.Since = now
		res.Consecutive = 1
	}
	r.results[p.Name] = res
	r.mu.Unlock()

	if transitioned && r.log != nil {
		level := slog.LevelInfo
		if state == StateDown {
			level = slog.LevelError
		}
		attrs := []slog.Attr{
			slog.String(logkey.Probe, p.Name),
			slog.String(logkey.ProbeState, string(state)),
			slog.Int64(logkey.LatencyMS, latency.Milliseconds()),
			slog.String(logkey.Component, p.Criticality.String()),
		}
		if err != nil {
			attrs = append(attrs, slog.String(logkey.Err, err.Error()))
		}
		r.log.LogAttrs(ctx, level, "readiness probe changed state", attrs...)
	}
}

// Snapshot is the readiness state at a moment.
type Snapshot struct {
	State    State
	Draining bool
	Results  []Result
	At       time.Time
}

// Failing returns the names of probes that are not up. Names only, so the
// result is safe to render on an unauthenticated endpoint.
func (s Snapshot) Failing() []string {
	var out []string
	for _, r := range s.Results {
		if r.State != StateUp {
			out = append(out, r.Name)
		}
	}
	return out
}

// Snapshot returns the current state.
func (r *Registry) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshotLocked()
}

func (r *Registry) snapshotLocked() Snapshot {
	now := r.clk.Now()
	out := Snapshot{State: StateUp, Draining: r.draining, At: now}

	for _, p := range r.probes {
		res, ok := r.results[p.Name]
		if !ok {
			res = Result{Name: p.Name, Criticality: p.Criticality, State: StateUnknown}
		} else if now.Sub(res.CheckedAt) > r.opts.StaleAfter {
			// A wedged poller must not leave a stale 200 behind: unknown
			// counts as failing.
			res.State = StateUnknown
			res.Err = errors.New("probe result is stale")
		}
		out.Results = append(out.Results, res)
	}
	sort.Slice(out.Results, func(i, j int) bool { return out.Results[i].Name < out.Results[j].Name })

	if r.draining {
		out.State = StateDown
		return out
	}
	for _, res := range out.Results {
		if res.State != StateUp && res.Criticality == Critical {
			out.State = StateDown
		}
	}
	return out
}

// Check forces an immediate re-check. For the admin listener, not for /readyz.
func (r *Registry) Check(ctx context.Context) Snapshot {
	r.mu.RLock()
	probes := append([]Probe(nil), r.probes...)
	r.mu.RUnlock()
	r.pollAll(ctx, probes)
	return r.Snapshot()
}

// SetDraining flips readiness to unready. Nothing sets it back: a process that
// has begun shutting down must never re-advertise itself, or a load balancer
// will send it traffic it is about to stop serving.
func (r *Registry) SetDraining() {
	r.mu.Lock()
	r.draining = true
	r.mu.Unlock()
}
