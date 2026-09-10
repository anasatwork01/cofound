// Package httpapi wires the api service's routes and readiness probes.
package httpapi

import (
	"context"
	"errors"
	"io"

	"github.com/anasatwork01/cofound/packages/chassis"
	"github.com/anasatwork01/cofound/packages/chassis/health"

	apiconfig "github.com/anasatwork01/cofound/services/api/internal/config"
)

// API holds the service's wiring.
//
// A value rather than package-level state, so two instances in one test binary
// cannot see each other's configuration.
type API struct{ Config *apiconfig.Binder }

// New returns the service wiring.
func New() *API { return &API{Config: apiconfig.NewBinder()} }

// Bind binds the service's configuration on the shared loader.
func (a *API) Bind(l *chassisLoader) { a.Config.Bind(l) }

// Setup mounts routes.
//
// This deliberately mounts NO /v1 routes yet. /healthz, /readyz and the
// envelope 404/405 all come from the chassis, and inventing an endpoint that
// api.openapi.yaml does not document would break working agreement 4 — the
// schema is the contract, and code running ahead of it is what makes the
// contract untrustworthy.
func (a *API) Setup(ctx context.Context, rt *chassis.Runtime) (io.Closer, error) {
	// The streaming subtree exists and is empty. It has no handler timeout and
	// no body cap, which is what makes it safe for SSE. Task 1.14 mounts
	// GET /v1/projects/{project}/sessions/{session}/events here.
	_ = rt.Mux.Stream

	return nil, nil
}

// Probes registers readiness probes.
//
// sandboxd is deliberately NOT probed. A readiness cascade turns one unready
// dependency into a cluster-wide outage: if api is unready because sandboxd is,
// every api replica leaves rotation at once. A peer's availability belongs in a
// request's 503, not in this instance's readiness.
func (a *API) Probes(rt *chassis.Runtime, reg *health.Registry) {
	cfg := a.Config.Config()

	// Task 0.6 replaces these with real pgx and Redis pings. They are
	// registered now, failing closed, so the readiness contract is exercised
	// from this task onward rather than appearing later.
	reg.Register(health.PingFunc("postgres", func(context.Context) error {
		if cfg.DatabaseURL == "" {
			return errors.New("DATABASE_URL is not configured")
		}
		return nil
	}, health.Critical))

	reg.Register(health.PingFunc("redis", func(context.Context) error {
		if cfg.RedisURL == "" {
			return errors.New("REDIS_URL is not configured")
		}
		return nil
	}, health.Critical))
}
