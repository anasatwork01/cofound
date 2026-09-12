// Command api is the REST API, auth, org/project CRUD, SSE gateway and
// approval gates.
package main

import (
	"github.com/anasatwork01/cofound/packages/chassis"
	"github.com/anasatwork01/cofound/packages/chassis/observability/sentry"
	"github.com/anasatwork01/cofound/packages/chassis/telemetry/otlp"

	"github.com/anasatwork01/cofound/services/api/internal/httpapi"
)

const service = "api"

// Build metadata, injected with -ldflags at build time. See the Makefile.
//
// These stay in package main because the Makefile injects -X main.version and
// -X main.commit. A chassis cannot own them without changing the ldflags paths,
// and moving them would silently produce "dev"/"unknown" in every production
// log line with no build failure.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	api := httpapi.New()

	chassis.Main(chassis.Service{
		Name:    service,
		Version: version,
		Commit:  commit,
		Bind:    api.Bind,
		Setup:   api.Setup,
		Probes:  api.Probes,
		// The console is a separate origin from the api — locally (3000 vs
		// 8080) and in production — and SPEC §8 puts the session in an httpOnly
		// cookie, so every console fetch is credentialed and cross-origin.
		// Without this the browser blocks every request before it is sent, which
		// is exactly what phase 0's acceptance criterion ran into.
		//
		// CONSOLE_ORIGIN, not a second variable: it is already required config,
		// already the origin every sign-in link is built from, and two vars for
		// one value is two things to get out of step.
		CORSOrigin: func() string { return api.Config.Config().ConsoleOrigin },
		// This one line opts api's binary into the OTLP exporter's ~65 modules
		// and ~10MB. gitd, aigw and mcp choose for themselves.
		Exporter: otlp.Factory,
		// And this one into Sentry (SPEC 17.3). It links sentry-go, but it
		// CONSTRUCTS nothing unless SENTRY_DSN resolves: with no DSN the factory
		// returns a nil reporter, which is a nil panic hook and a no-op
		// shutdown. That is deliberate — sentry-go's own Init with an empty DSN
		// leaves a 100ms ticker running for the life of the process.
		//
		// Errors only. Spans already reach Sentry through the OTLP endpoint
		// above if that is where the collector points, and panics are captured
		// by the chassis's one recoverer rather than by sentryhttp.
		Reporter: sentry.Factory,
	})
}
