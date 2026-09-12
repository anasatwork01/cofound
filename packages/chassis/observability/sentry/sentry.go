// Package sentry is the Sentry error reporter, deliberately isolated in its own
// leaf package exactly as telemetry/otlp is.
//
// Go links per package, so a service that never imports this one links no
// sentry-go. Import it from a service main, never from chassis code.
//
// # Scope: errors only
//
// This package reports GROUPED, DEDUPLICATED ISSUES WITH GO STACK TRACES, and
// nothing else. It deliberately does NOT use:
//
//   - sentry-go/otel/otlp — Sentry tracing is now an OTLP endpoint, which
//     telemetry.ExporterFactory already accommodates. Sending spans twice buys
//     nothing.
//   - sentry-go/http — sentryhttp's Repanic defaults to FALSE, so it swallows
//     the panic that httpx.Recover exists to turn into an error envelope, and
//     it opens a second "http.server" transaction on top of otelhttp's. One
//     recoverer, one envelope, one span.
//   - sentry-go/slog — the logging pipeline already redacts and ships; a second
//     sink for the same lines is a second place for user content to escape.
//
// The whole OpenTelemetry story here is one line: sentryotel's linking
// integration, which resolves the active trace id from the context so a
// captured issue and its trace point at each other.
package sentry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	sentrygo "github.com/getsentry/sentry-go"
	sentryotel "github.com/getsentry/sentry-go/otel"

	"github.com/anasatwork01/cofound/packages/chassis/observability"
)

// Config is observability.Config. The alias keeps one struct for the seam and
// its implementation: a service main that builds one by hand does not have to
// know which package named it.
type Config = observability.Config

// Handle owns the process's Sentry client and its shutdown.
//
// The zero value is not usable; a nil *Handle is, and every method tolerates
// one. That is what makes "no DSN" a value rather than a branch at every call
// site.
type Handle struct {
	client *sentrygo.Client

	once sync.Once
	err  error
}

// Init binds a Sentry client to the current hub, returning nil when no DSN is
// configured.
//
// THE NIL RETURN IS THE WHOLE POINT. sentry-go's Init with an empty DSN does
// not become a no-op: NewClient still builds the telemetry processor, whose
// Scheduler.Start spawns a worker goroutine plus a second goroutine holding a
// 100ms time.Ticker, for the life of the process. Multiplied by every service,
// in every environment that has no DSN — local development, CI, and any
// self-hosted deploy — that is a permanent timer wake-up per service buying
// nothing, and it is invisible: no log line, no error, no behavioural
// difference to notice. sentry_test.go counts goroutines across this call so
// the invisible stays impossible.
//
// A DSN that is present but malformed FAILS instead, because the alternative is
// a production service that boots happily and reports nothing. That matches the
// config layer: a bad deploy dies before it takes traffic.
func Init(ctx context.Context, c Config) (*Handle, error) {
	if strings.TrimSpace(c.DSN) == "" {
		return nil, nil
	}
	// ctx is honoured only as a cancellation check. Init does no I/O — the
	// transport connects lazily, like the OTLP exporter — so there is nothing
	// else here to bound.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sentry: %w", err)
	}
	if err := sentrygo.Init(options(c)); err != nil {
		// Never %w-wrap into the message with the DSN in it: sentry-go's DSN
		// parse errors quote the value.
		return nil, errors.New("sentry: the configured DSN was rejected: " + redactDSN(err.Error(), c.DSN))
	}
	return &Handle{client: sentrygo.CurrentHub().Client()}, nil
}

// Factory adapts Init to observability.ReporterFactory, and is the one line a
// service main writes.
//
// The explicit nil return is load-bearing: `return Init(ctx, c)` would compile
// and would hand the chassis a non-nil interface wrapping a nil *Handle, so
// every "is a reporter configured?" check would answer yes with no DSN set.
func Factory(ctx context.Context, c Config) (observability.Reporter, error) {
	h, err := Init(ctx, c)
	if err != nil || h == nil {
		return nil, err
	}
	return h, nil
}

var _ observability.ReporterFactory = Factory

// CapturePanic reports a recovered panic as a fatal issue.
//
// It is called from inside httpx.Recover's deferred function, so the live stack
// still holds the panicking frames and sentry-go's own NewStacktrace picks them
// up — which is why no stack is passed in. The hub comes from ctx when one is
// bound there, so a future per-request scope works without changing this.
func (h *Handle) CapturePanic(ctx context.Context, value any) {
	if h == nil || h.client == nil || value == nil {
		return
	}
	hub := sentrygo.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentrygo.CurrentHub()
	}
	hub.RecoverWithContext(ctx, value)
}

// Shutdown flushes buffered events and then releases the client, bounded by ctx.
//
// The two halves are bounded differently because sentry-go bounds them
// differently. FlushWithContext honours the context. Client.Close does NOT take
// one: it hardcodes a 5s timeout and spends it twice — Scheduler.Stop flushes
// for the timeout and then waits for the timeout again — so a Close on a
// wedged network can hold the process for ~10s. lifecycle.PhaseTelemetry has a
// budget measured in single-digit seconds and the orchestrator's grace period
// behind it, so Close runs on a goroutine this function ABANDONS when the
// budget expires. Abandoning it is safe and deliberate: the process is exiting,
// and the alternative is being SIGKILLed mid-write.
func (h *Handle) Shutdown(ctx context.Context) error {
	if h == nil || h.client == nil {
		return nil
	}
	h.once.Do(func() { h.err = h.shutdown(ctx) })
	return h.err
}

func (h *Handle) shutdown(ctx context.Context) error {
	flushed := h.client.FlushWithContext(ctx)

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		h.client.Close()
	}()

	select {
	case <-closed:
	case <-ctx.Done():
		return fmt.Errorf("sentry: abandoned Close after the telemetry budget expired: %w", ctx.Err())
	}
	if !flushed {
		return errors.New("sentry: flush did not finish inside the telemetry budget; some issues were dropped")
	}
	return nil
}

// options builds the client options.
//
// Unexported on purpose: these are policy, not configuration. Two of them are
// SPEC 17 controls rather than preferences, and both are commented where they
// are made.
func options(c Config) sentrygo.ClientOptions {
	return sentrygo.ClientOptions{
		Dsn: c.DSN,

		// Explicit, always non-empty. An empty Environment makes NewClient read
		// SENTRY_ENVIRONMENT; an empty Release makes it walk thirteen CI
		// variables and then shell out to `git describe`. Either would let the
		// deployment environment, not the chassis, decide what an issue is
		// filed against — and release regression detection is the second of the
		// two reasons this package exists.
		Environment: env(c),
		Release:     release(c),

		// The reason for the first. A panic value is very often a string
		// (panic("boom")), and a string goes to EventFromMessage, which attaches
		// a stack trace ONLY when this is true. Without it the headline feature
		// — a Go stack trace on the issue — silently does not happen for the
		// most common panic shape.
		AttachStacktrace: true,

		// SPEC 17, not a preference. Both fields are LEFT AT THEIR ZERO VALUES
		// and must stay there.
		//
		// sentry-go 0.48.0's changelog recommends passing
		// &sentry.DataCollection{} to adopt the new granular options. Do not.
		// A nil DataCollection resolves through legacyDataCollection(false):
		// cookies CollectionOff, HTTPBodies empty, UserInfo false. An empty
		// &DataCollection{} resolves through resolveDataCollection: cookies on
		// under a denylist and ALL THREE HTTP body types collected. The
		// console's session cookie is a __Host- cookie and request bodies carry
		// user content, so the "recommended" one-line upgrade is what ships both
		// to a third party. SPEC 17.3 requires structured output with no secret
		// values and no user content; sentry_test.go asserts the RESOLVED
		// collection, so the guard survives a future default change too.
		DataCollection: nil,
		SendDefaultPII: false,

		ServerName: c.InstanceID,
		Tags:       tags(c),

		// The entire OpenTelemetry story: resolve the active trace id from the
		// context so a captured issue links to its trace.
		Integrations: func(defaults []sentrygo.Integration) []sentrygo.Integration {
			return append(defaults, sentryotel.NewOtelIntegration())
		},
	}
}

func env(c Config) string {
	if c.Env == "" {
		return "development"
	}
	return c.Env
}

// release is service@version+commit, the shape Sentry's release UI expects.
//
// It is never empty — see options. "dev"/"unknown" are the Makefile's own
// placeholders for an un-tagged local build, and keeping them is better than
// falling back to sentry-go's detection: a release that reads api@dev is
// obviously a local build, whereas one silently taken from GITHUB_SHA looks
// authoritative and is not.
func release(c Config) string {
	name := c.Service
	if name == "" {
		name = "halyard"
	}
	version := c.Version
	if version == "" {
		version = "dev"
	}
	out := name + "@" + version
	if c.Commit != "" && c.Commit != "unknown" {
		out += "+" + c.Commit
	}
	return out
}

func tags(c Config) map[string]string {
	out := map[string]string{}
	if c.Service != "" {
		out["service"] = c.Service
	}
	if c.Commit != "" {
		out["commit"] = c.Commit
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// redactDSN keeps a DSN out of a boot error. sentry-go's parse failures quote
// the value they rejected, and the boot error is printed to stderr by
// chassis.Main.
func redactDSN(msg, dsn string) string {
	if dsn == "" {
		return msg
	}
	return strings.ReplaceAll(msg, dsn, "[redacted]")
}
