package httpx

import (
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/anasatwork01/cofound/packages/chassis/health"
	"github.com/anasatwork01/cofound/packages/chassis/lifecycle"
)

// RouterConfig configures the assembled router.
type RouterConfig struct {
	Service string
	Env     string
	Version string
	Commit  string

	Log    *slog.Logger
	Errors *ErrorWriter
	Health *health.Registry
	Drain  *lifecycle.Drain

	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider

	RequestID      RequestIDConfig
	TrustedProxies []netip.Prefix

	HandlerTimeout  time.Duration
	MaxBodyBytes    int64
	ReadinessDetail bool

	// PublicEndpoint feeds otelhttp.WithPublicEndpointFn, which applies
	// trace.WithNewRoot() and demotes an untrusted caller's span context to a
	// Link. That is the SPEC 17 sandbox trust-boundary control: a sandbox can
	// forge a traceparent to graft its spans onto another tenant's trace.
	// Exposed for task 1.17; unused in 0.4.
	PublicEndpoint func(*http.Request) bool

	// PanicHook reports a recovered panic to an error reporter. nil means
	// none, which is what every test and every service without a SENTRY_DSN
	// gets.
	PanicHook PanicHook
}

// Mux is the assembled router and its three subtrees.
//
// Returning them separately is deliberate: a service CANNOT accidentally put
// the handler timeout or the body cap on a stream, because the streaming
// subtree simply does not have them. The SSE exclusion becomes a reviewable
// fact rather than a rule someone has to remember.
type Mux struct {
	Root   *chi.Mux
	Public chi.Router
	Authed chi.Router
	Stream chi.Router

	cfg RouterConfig
}

// Router assembles the middleware stack.
//
// Order, outermost to innermost, and why each position:
//  1. otelhttp        — span covers full server wall-clock, and every inner log
//     line can carry trace_id.
//  2. Record          — wraps otelhttp's writer while still exposing Unwrap.
//  3. RequestID       — must precede anything that logs or names a span attr.
//  4. ClientIP        — needs the raw peer before any handler acts.
//  5. RouteTag        — post-routing by construction.
//  6. LogFields       — seeds the route once routing is known.
//  7. AccessLog       — deferred single line; OUTSIDE Recover so it still fires
//     during a panic unwind.
//  8. Recover         — INSIDE tracing and logging. See recover.go.
func Router(cfg RouterConfig) *Mux {
	if cfg.Errors == nil {
		cfg.Errors = NewErrorWriter(cfg.Log)
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}

	root := chi.NewMux()

	// chi's defaults are http.NotFound (text/plain) and a plain-text 405,
	// which every generated client decoder fails to parse.
	root.NotFound(cfg.Errors.NotFound())
	root.MethodNotAllowed(cfg.Errors.MethodNotAllowed())

	root.Use(Record)
	root.Use(RequestID(cfg.RequestID))
	root.Use(ClientIP(cfg.TrustedProxies))
	root.Use(RouteTag)
	root.Use(LogFields)
	root.Use(AccessLog(log))
	root.Use(Recover(cfg.Errors, cfg.PanicHook))

	m := &Mux{Root: root, cfg: cfg}

	// Health sits on the root, before auth, so a probe needs no credential.
	if cfg.Health != nil {
		MountHealth(root, cfg.Health, HealthInfo{
			Service: cfg.Service, Version: cfg.Version, Commit: cfg.Commit,
		}, cfg.ReadinessDetail)
	}

	root.Group(func(r chi.Router) {
		r.Use(Timeout(cfg.HandlerTimeout, cfg.Errors))
		r.Use(MaxBytes(cfg.MaxBodyBytes))
		m.Public = r
	})
	root.Group(func(r chi.Router) {
		r.Use(Timeout(cfg.HandlerTimeout, cfg.Errors))
		r.Use(MaxBytes(cfg.MaxBodyBytes))
		// A service adds auth and tenancy here with Use.
		m.Authed = r
	})
	root.Group(func(r chi.Router) {
		// NO handler timeout and NO body cap. A context timeout kills SSE just
		// as surely as a write deadline; exclusion is the mechanism.
		m.Stream = r
	})
	return m
}

// Handler returns the fully wrapped handler.
//
// There is deliberately NO WithSpanNameFormatter. otelhttp v0.71.0 renames the
// span after the handler when r.Pattern is set, and chi v5.3.2 sets it, so the
// DEFAULT formatter already yields low-cardinality names like
// "GET /v1/projects/{project}/sessions/{session}". The obvious custom
// r.URL.Path formatter — what most examples show — silently turns every span
// name into a unique high-cardinality string, and the clobbering happens inside
// otelhttp's own deferred code, so it is very hard to debug.
func (m *Mux) Handler() http.Handler {
	opts := []otelhttp.Option{}
	if m.cfg.TracerProvider != nil {
		opts = append(opts, otelhttp.WithTracerProvider(m.cfg.TracerProvider))
	}
	if m.cfg.MeterProvider != nil {
		opts = append(opts, otelhttp.WithMeterProvider(m.cfg.MeterProvider))
	}
	if m.cfg.PublicEndpoint != nil {
		opts = append(opts, otelhttp.WithPublicEndpointFn(m.cfg.PublicEndpoint))
	}
	name := m.cfg.Service
	if name == "" {
		name = "http"
	}
	return otelhttp.NewHandler(m.Root, name, opts...)
}

// ServeHTTP implements http.Handler using the full stack.
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.Handler().ServeHTTP(w, r)
}

var _ http.Handler = (*Mux)(nil)
