// Package db is the control plane's Postgres access layer.
//
// Two things here are load-bearing and neither is obvious from the pgx API.
//
// # The pool refuses to start as a privileged role
//
// Row-level security is the tenant isolation control (SPEC §6), and it is inert
// for a role that owns the tables, or that has BYPASSRLS, or that is a
// superuser. Verified on Postgres 18.6: connected as the owner, a policy-scoped
// query returns every row of every tenant, with no error and no warning. The
// local `halyard` role in compose.yaml is exactly such a role, so a service
// configured with the migration credentials would have no isolation while
// looking perfectly healthy.
//
// There is no way to detect that from the application's own queries — they
// simply return more rows than they should — so Open checks the role's
// attributes at startup and refuses to return a pool. A service that cannot
// isolate tenants must not serve traffic.
//
// # Every tenant query runs inside Scope
//
// The policy reads `app.org_id`, which has to be set per transaction. Setting
// it outside a transaction on a pooled connection leaves it set for whoever
// borrows that connection next — verified: a plain SET persists for the whole
// session. Scope is therefore the only exported way to run tenant-scoped work,
// it always opens a transaction, and it uses set_config(..., is_local => true)
// so the value is discarded at commit or rollback.
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoRows is pgx's sentinel, re-exported so callers need not import pgx to
// check for it. packages/chassis/errs matches on the message text because the
// chassis has no database dependency; a service that imports this package
// should use errors.Is against this instead.
var ErrNoRows = pgx.ErrNoRows

// ErrPrivilegedRole is returned by Open when the connecting role would bypass
// row-level security.
var ErrPrivilegedRole = errors.New("db: the connecting role bypasses row-level security")

// Config describes the pool. Field names match the environment variables a
// service binds them from, so the two can be read side by side.
type Config struct {
	// URL is the libpq connection string. Required.
	URL string

	// MaxConns bounds the pool. Sized against the database's own limit divided
	// by the number of replicas, not against expected concurrency: a pool that
	// can open more connections than Postgres will accept turns a traffic spike
	// into "too many clients already" for every service at once.
	MaxConns int32

	// MinConns keeps connections warm. Establishing a TLS connection to a
	// managed Postgres costs tens of milliseconds, and a control plane's traffic
	// is bursty enough that a cold pool shows up as latency on the first request
	// after an idle period.
	MinConns int32

	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration

	// StatementCacheMode selects pgx's query exec mode.
	//
	// "prepared" is fastest and is wrong behind a transaction-mode pooler:
	// PgBouncer, or whichever pooler the chosen container host puts in the
	// path, may route consecutive statements to different server connections,
	// so a prepared statement created on one is missing on the next. The
	// failure is intermittent and looks like a database fault. Set this to
	// "simple" or "exec" when a pooler sits in front.
	//
	// Not Hyperdrive: that is a Workers binding, and the control plane reaches
	// Postgres over pgx from a container. A Worker holding the control-plane
	// database credential would invert SPEC §17. See docs/verified.md §22
	// item 4.
	StatementCacheMode string

	// ApplicationName reaches pg_stat_activity, which is where an operator looks
	// first when the database is busy and they need to know which service is
	// responsible.
	ApplicationName string

	// AllowPrivilegedRole disables the startup refusal described in the package
	// comment. It exists for the migration runner and for tests that must act as
	// the owner, and it is deliberately awkward to reach.
	AllowPrivilegedRole bool
}

// Defaults fills in the values a service should not have to think about.
func Defaults() Config {
	return Config{
		MaxConns:           10,
		MinConns:           2,
		MaxConnLifetime:    30 * time.Minute,
		MaxConnIdleTime:    5 * time.Minute,
		HealthCheckPeriod:  30 * time.Second,
		StatementCacheMode: "prepared",
	}
}

// Pool is a Postgres connection pool with tenant scoping.
type Pool struct {
	pool *pgxpool.Pool
	cfg  Config
}

// Open connects, verifies the role cannot bypass row-level security, and
// returns a pool.
func Open(ctx context.Context, cfg Config) (*Pool, error) {
	if cfg.URL == "" {
		return nil, errors.New("db: URL is required")
	}
	pcfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		// The error from ParseConfig can contain the connection string, and the
		// connection string contains the password.
		return nil, errors.New("db: connection string could not be parsed")
	}

	if cfg.MaxConns > 0 {
		pcfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		pcfg.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		pcfg.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		pcfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	}
	if cfg.HealthCheckPeriod > 0 {
		pcfg.HealthCheckPeriod = cfg.HealthCheckPeriod
	}
	if mode, ok := execMode(cfg.StatementCacheMode); ok {
		pcfg.ConnConfig.DefaultQueryExecMode = mode
	} else if cfg.StatementCacheMode != "" {
		return nil, fmt.Errorf("db: unknown statement cache mode %q; want prepared, cache_describe, describe_exec, exec or simple", cfg.StatementCacheMode)
	}
	if cfg.ApplicationName != "" {
		if pcfg.ConnConfig.RuntimeParams == nil {
			pcfg.ConnConfig.RuntimeParams = map[string]string{}
		}
		pcfg.ConnConfig.RuntimeParams["application_name"] = cfg.ApplicationName
	}

	// Spans for every query, with the same tracer the chassis installs. Attached
	// here rather than left to callers so a service cannot forget it.
	pcfg.ConnConfig.Tracer = otelpgx.NewTracer()

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("db: pool: %w", err)
	}

	p := &Pool{pool: pool, cfg: cfg}
	if !cfg.AllowPrivilegedRole {
		if err := p.assertUnprivileged(ctx); err != nil {
			pool.Close()
			return nil, err
		}
	}
	return p, nil
}

func execMode(name string) (pgx.QueryExecMode, bool) {
	switch name {
	case "prepared":
		return pgx.QueryExecModeCacheStatement, true
	case "cache_describe":
		return pgx.QueryExecModeCacheDescribe, true
	case "describe_exec":
		return pgx.QueryExecModeDescribeExec, true
	case "exec":
		return pgx.QueryExecModeExec, true
	case "simple":
		return pgx.QueryExecModeSimpleProtocol, true
	}
	return 0, false
}

// assertUnprivileged refuses a role that row-level security would not apply to.
//
// Checks the three independent ways a role escapes a policy. Table ownership is
// the fourth and is not checked here: `force row level security` in migration
// 00013 covers the owner, and enumerating ownership of thirty-four tables at
// every boot would cost more than it catches.
func (p *Pool) assertUnprivileged(ctx context.Context) error {
	var role string
	var super, bypass bool
	err := p.pool.QueryRow(ctx,
		`select current_user, rolsuper, rolbypassrls
		   from pg_roles where rolname = current_user`).Scan(&role, &super, &bypass)
	if err != nil {
		return fmt.Errorf("db: could not read the connecting role's attributes: %w", err)
	}
	if super || bypass {
		return fmt.Errorf(
			"%w: %q has%s%s, so every row-level security policy is inert and "+
				"tenant isolation is silently absent. Connect as a dedicated "+
				"unprivileged role (migration 00013 creates halyard_app), or set "+
				"AllowPrivilegedRole for a migration runner",
			ErrPrivilegedRole, role,
			pick(super, " SUPERUSER", ""), pick(bypass, " BYPASSRLS", ""))
	}
	return nil
}

func pick(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}

// Close waits for in-flight work and shuts the pool down. Register it with the
// chassis shutdown so it runs in the ordered close phase.
func (p *Pool) Close() { p.pool.Close() }

// Ping is the readiness check.
//
// `execute("select 1")` with no arguments, which takes the simple-query path
// and so does not add an entry to the statement cache. A health check should not
// evict a real query's prepared statement.
func (p *Pool) Ping(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, "select 1")
	return err
}

// Unscoped exposes the pool for queries that are deliberately not tenant-scoped:
// sign-in by email before any org is known, and the global catalogue.
//
// Named to be conspicuous in review. Every other read must go through Scope, and
// a query here is one that row-level security will not protect.
func (p *Pool) Unscoped() *pgxpool.Pool { return p.pool }

// Stat reports pool utilisation, for the metrics endpoint.
func (p *Pool) Stat() *pgxpool.Stat { return p.pool.Stat() }
