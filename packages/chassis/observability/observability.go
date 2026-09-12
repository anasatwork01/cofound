// Package observability is the error-reporting seam.
//
// SPEC 17.3 asks for "Sentry for both the console and services". This package
// is the part of that the chassis core is allowed to know about: an interface
// and a factory type, no vendor SDK. observability/sentry implements it, and a
// service main opts its binary in with one line — exactly the shape
// telemetry/otlp already uses for spans.
//
// The seam lives in its own package rather than in chassis because httpx needs
// the panic half of it and chassis imports httpx; a type in chassis would be an
// import cycle.
//
// Scope is deliberately narrow: ERRORS ONLY. Sentry tracing is now an OTLP
// endpoint, which telemetry.ExporterFactory already accommodates, so routing
// spans through Sentry would duplicate OpenTelemetry for no gain. What Sentry
// gives that OTel and structured logs do not is grouped, deduplicated issues
// carrying Go stack traces, and regression detection across releases.
package observability

import "context"

// Config is how a process identifies itself to an error reporter.
//
// Every field is passed EXPLICITLY, and that is the point of the struct. Left
// alone, sentry-go reads SENTRY_DSN and SENTRY_ENVIRONMENT out of the process
// environment itself and derives a release from GITHUB_SHA, SENTRY_RELEASE,
// eleven other CI variables, the build info, or by shelling out to
// `git describe`. None of those should decide which Halyard service, in which
// environment, at which release, an incident is filed against — the chassis
// already knows all three.
type Config struct {
	// DSN empty means "no reporter". It is the ONLY switch: a reporter must
	// never be constructed without one. See observability/sentry.Init.
	DSN string

	Service    string
	Version    string
	Commit     string
	Env        string
	InstanceID string
}

// Reporter is grouped, deduplicated issue reporting for one process.
//
// It is intentionally two methods. Anything else an error backend can do —
// breadcrumbs, transactions, HTTP middleware, log forwarding — is either
// already covered by OpenTelemetry and the logging pipeline or would ship user
// content to a third party, so it is not on this interface and cannot be
// reached through it.
type Reporter interface {
	// CapturePanic reports a recovered panic. It is called from inside the
	// deferred recover, so the live goroutine stack still contains the
	// panicking frames and the backend can symbolise them.
	//
	// ctx carries the active OpenTelemetry span, which is how a captured
	// issue and its trace find each other.
	//
	// It must never panic and must never block a request: it runs on the
	// request's own goroutine during the unwind.
	CapturePanic(ctx context.Context, value any)

	// Shutdown flushes buffered events and releases the transport, bounded by
	// ctx. The chassis calls it in lifecycle.PhaseTelemetry, last, so the
	// drain's own failures are still reported.
	Shutdown(ctx context.Context) error
}

// ReporterFactory builds a Reporter. nil in chassis.Service links none.
//
// A factory MUST return a nil INTERFACE when reporting is disabled, never a
// typed nil pointer in an interface: `var r Reporter = (*Handle)(nil)` compares
// != nil, and the chassis's "is a reporter configured?" check would then be
// wrong in exactly the case — no DSN — that has to be right.
type ReporterFactory func(context.Context, Config) (Reporter, error)
