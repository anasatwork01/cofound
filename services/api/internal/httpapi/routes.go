// Package httpapi wires the api service's routes and readiness probes.
package httpapi

import (
	"context"
	"errors"
	"io"

	"github.com/anasatwork01/cofound/packages/chassis"
	"github.com/anasatwork01/cofound/packages/chassis/config"
	"github.com/anasatwork01/cofound/packages/chassis/health"
	"github.com/anasatwork01/cofound/packages/chassis/httpx"
	"github.com/anasatwork01/cofound/packages/db"

	"github.com/anasatwork01/cofound/services/api/internal/audit"
	"github.com/anasatwork01/cofound/services/api/internal/auth"
	"github.com/anasatwork01/cofound/services/api/internal/idempotency"
	"github.com/anasatwork01/cofound/services/api/internal/tenancy"
	"github.com/anasatwork01/cofound/services/api/internal/v1"

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

	// Auth is the sign-in surface, built by Setup.
	Auth *auth.Handlers

	// Tenancy resolves (user_id, org_id, role) for the authed subtree.
	Tenancy *tenancy.Resolver

	// V1 is the org and project surface.
	V1 *v1.Handlers

	// MailerFor overrides how magic links are delivered. Tests capture the link
	// with it; production leaves it nil and gets the log-only mailer, because
	// SPEC §8 requires email magic links and neither §3's stack nor §22's list
	// names a transactional email provider. See docs/open-questions.md Q5.
	MailerFor func(*chassis.Runtime) auth.Mailer

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
	// container host: behind a transaction-mode pooler (PgBouncer, or whatever
	// the host provides) prepared statements break intermittently, and picking
	// the safe-but-slower mode before knowing whether a pooler is in the path
	// would be guessing in the expensive direction. docs/verified.md records
	// the constraint. Hyperdrive is not a candidate here — it is a Workers
	// binding and this pool lives in a container (§22 item 4).

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

	// Sign-in (SPEC §8). Mounted on the PUBLIC subtree, including "who am I"
	// and sign-out: these are the endpoints that establish who the caller is,
	// so they cannot sit behind middleware that needs that answer already.
	//
	// Secure cookies everywhere but development. Derived from the environment
	// rather than configured separately, so the two cannot disagree — and a
	// __Host- cookie without Secure is silently dropped by the browser, which
	// presents as "sign-in does nothing" with no error anywhere.
	a.Auth = &auth.Handlers{
		Sessions: auth.NewSessions(pool, auth.SessionPolicy{
			IdleTimeout:     cfg.SessionIdleTimeout,
			AbsoluteTimeout: cfg.SessionAbsoluteTimeout,
			RotateAfter:     cfg.SessionRotateAfter,
			RotateGrace:     auth.DefaultSessionPolicy().RotateGrace,
		}, nil),
		Links: auth.NewMagicLinks(pool, a.Mailer(rt), auth.DefaultMagicLinkPolicy(),
			cfg.ConsoleOrigin, nil),
		Google: auth.NewGoogle(pool, auth.GoogleConfig{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			RedirectURL:  cfg.GoogleRedirectURL,
		}, nil, nil),
		Pool:          pool,
		Cookies:       auth.CookieConfig{Secure: rt.Config.Env != config.EnvDevelopment, MaxAge: cfg.SessionAbsoluteTimeout},
		Audit:         auth.DBAudit{Pool: pool},
		ConsoleOrigin: cfg.ConsoleOrigin,
	}
	// Rate limits on the unauthenticated surface, keyed by peer.
	//
	// In-process, which divides the real limit by the number of replicas. That
	// is correct for protecting one process and wrong for enforcing a quota, so
	// the store is an interface and the shared implementation waits on SPEC §21
	// decision 1 — the container host determines what it should be. Recorded in
	// docs/open-questions.md rather than guessed at.
	//
	// Applied to the whole public subtree, not only to sign-in: an
	// unauthenticated endpoint is by definition one anyone can reach.
	rt.Mux.Public.Use((&httpx.RateLimiter{
		// Ten a second sustained, thirty at once. A console page load is a
		// dozen requests in a few hundred milliseconds and is not abuse; a
		// script is.
		Limit:  httpx.RateLimit{Rate: 10, Burst: 30},
		Key:    httpx.KeyByIP,
		Errors: rt.Errors,
	}).Middleware)

	a.Auth.Mount(rt.Mux.Public, rt.Errors)

	// Tenancy (SPEC §8): every authenticated request resolves to
	// (user_id, org_id, role) exactly once, here, and role checks happen in
	// tenancy.Require rather than in any handler. Mounted on the AUTHED subtree
	// so that a route added later is covered by construction — a handler
	// mounted there cannot forget to authenticate, because it has no way to be
	// reached without this middleware having run. The org and the role are
	// resolved per route by tenancy.Resolver.Require, which must run after
	// chi has matched the pattern carrying {org} or {project}.
	//
	// No /v1 routes are mounted on it yet. Task 0.9 adds the first of them; the
	// middleware and its matrix are tested directly in the meantime, because
	// inventing an endpoint api.openapi.yaml does not document to give the test
	// something to call would break working agreement 4.
	a.Tenancy = &tenancy.Resolver{Pool: pool, Auth: a.Auth}
	rt.Mux.Authed.Use(a.Tenancy.Authenticate)

	// And on the authenticated surface, keyed by tenant so one org cannot
	// exhaust another's allowance. Higher, because a signed-in console is
	// legitimately chatty.
	rt.Mux.Authed.Use((&httpx.RateLimiter{
		Limit:  httpx.RateLimit{Rate: 50, Burst: 200},
		Key:    httpx.KeyByOrg,
		Errors: rt.Errors,
	}).Middleware)

	// The /v1 surface. Every route declares its required role beside itself, so
	// the permission a handler runs under is readable from one function rather
	// than from the handler bodies.
	a.V1 = &v1.Handlers{
		Pool:       pool,
		Tenancy:    a.Tenancy,
		Audit:      audit.New(pool),
		InviteTTL:  v1.DefaultInviteTTL(),
		Idempotent: idempotency.New(pool, a.Tenancy).Wrap,
	}
	a.V1.Mount(rt.Mux.Authed, rt.Errors)

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

// Mailer returns the configured mailer, or the log-only default.
//
// The default refuses to be useful in production: it writes the link as user
// content, which the chassis redactor replaces above debug level, so a
// production deploy that reached it does not spray live sign-in links into a
// log pipeline. It is still the wrong thing to ship, which is why Q5 is open.
func (a *API) Mailer(rt *chassis.Runtime) auth.Mailer {
	if a.MailerFor != nil {
		return a.MailerFor(rt)
	}
	return auth.LogMailer{Log: rt.Log}
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
