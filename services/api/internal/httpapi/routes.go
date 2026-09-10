// Package httpapi wires the api service's routes and readiness probes.
package httpapi

import (
	"context"
	"errors"
	"io"

	"github.com/anasatwork01/cofound/packages/chassis"
	"github.com/anasatwork01/cofound/packages/chassis/health"
	"github.com/anasatwork01/cofound/packages/db"

	apiconfig "github.com/anasatwork01/cofound/services/api/internal/config"
)

// API holds the service's wiring.
//
// A value rather than package-level state, so two instances in one test binary
// cannot see each other's configuration.
type API struct {
	Config *apiconfig.Binder

	// DB is nil until Setup runs, and Probes runs after Setup.
	DB *db.Pool

	// Open is the seam a test uses to boot without Postgres.
	//
	// It exists because the boot tests assert on routing, the error envelope
	// and the configuration boot line -- none of which involve a query -- and
	// requiring a live database for those would either put Postgres in the
	// `go` CI job or skip them by default. Both are worse than one field.
	//
	// It is NOT a way to run production without the startup checks: the default
	// is db.Open, and a test that substitutes something else gets a service
	// whose postgres probe reports unready, which is what
	// TestReadyzIs503WhenAProbeIsDown relies on.
	Open func(context.Context, db.Config) (*db.Pool, error)
}

// New returns the service wiring.
func New() *API {
	return &API{Config: apiconfig.NewBinder(), Open: db.Open}
}

// Bind binds the service's configuration on the shared loader.
func (a *API) Bind(l *chassisLoader) { a.Config.Bind(l) }

// Setup opens the database pool and mounts routes.
//
// This deliberately mounts NO /v1 routes yet. /healthz, /readyz and the
// envelope 404/405 all come from the chassis, and inventing an endpoint that
// api.openapi.yaml does not document would break working agreement 4 — the
// schema is the contract, and code running ahead of it is what makes the
// contract untrustworthy.
//
// The pool is opened here rather than lazily on first use so that a bad
// connection string, an unreachable database or — most importantly — a
// privileged role fails the boot with a clear message, instead of surfacing as
// a 500 on whichever endpoint a user happened to hit first.
func (a *API) Setup(ctx context.Context, rt *chassis.Runtime) (io.Closer, error) {
	cfg := a.Config.Config()

	dbCfg := db.Defaults()
	dbCfg.URL = cfg.DatabaseURL
	dbCfg.ApplicationName = rt.Config.Service
	// StatementCacheMode stays at "prepared" until §21 decision 1 settles the
	// container host: behind a transaction-mode pooler (PgBouncer, Hyperdrive)
	// prepared statements break intermittently, and picking the safe-but-slower
	// mode before knowing whether a pooler is in the path would be guessing in
	// the expensive direction. docs/verified.md records the constraint.

	open := a.Open
	if open == nil {
		open = db.Open
	}
	pool, err := open(ctx, dbCfg)
	if err != nil {
		return nil, err
	}
	a.DB = pool
	if pool == nil {
		// A test's opener may decline to connect. Probes then reports the
		// postgres probe unready, which is honest: there is no database.
		return nil, nil
	}

	// The streaming subtree exists and is empty. It has no handler timeout and
	// no body cap, which is what makes it safe for SSE. Task 1.14 mounts
	// GET /v1/projects/{project}/sessions/{session}/events here.
	_ = rt.Mux.Stream

	return closer{pool}, nil
}

// closer adapts the pool to io.Closer.
//
// pgxpool.Close returns nothing because it cannot fail: it waits for in-flight
// work and drops the connections. The chassis wants an io.Closer so that the
// ordered shutdown phase has one shape for every dependency.
type closer struct{ pool *db.Pool }

func (c closer) Close() error {
	c.pool.Close()
	return nil
}

// Probes registers readiness probes.
//
// sandboxd is deliberately NOT probed. A readiness cascade turns one unready
// dependency into a cluster-wide outage: if api is unready because sandboxd is,
// every api replica leaves rotation at once. A peer's availability belongs in a
// request's 503, not in this instance's readiness.
func (a *API) Probes(rt *chassis.Runtime, reg *health.Registry) {
	cfg := a.Config.Config()

	// `select 1` through the pool. It takes the simple-query path, so unlike a
	// prepared `select 1` it does not occupy a slot in the statement cache — a
	// health check polled every couple of seconds by every replica should not
	// evict a real query's plan.
	reg.Register(health.PingFunc("postgres", func(ctx context.Context) error {
		if a.DB == nil {
			return errors.New("the database pool was not opened")
		}
		return a.DB.Ping(ctx)
	}, health.Critical))

	// Redis stays a configuration check until task 1.12 introduces the write
	// lease, which is the first thing in this service that actually needs it.
	// Registering it now, failing closed, keeps the readiness contract honest:
	// a deploy that forgets REDIS_URL reports unready rather than working until
	// the first lease acquisition.
	reg.Register(health.PingFunc("redis", func(context.Context) error {
		if cfg.RedisURL == "" {
			return errors.New("REDIS_URL is not configured")
		}
		return nil
	}, health.Critical))
}
