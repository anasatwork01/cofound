package db_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anasatwork01/cofound/packages/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ownerURL is the migration role: superuser, BYPASSRLS. Used only to seed and
// to prove that Open refuses it.
func ownerURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	skipOrFail(t, "DATABASE_URL")
	return ""
}

// appURL is the role services connect as: no superuser, no BYPASSRLS.
func appURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("APP_DATABASE_URL"); v != "" {
		return v
	}
	skipOrFail(t, "APP_DATABASE_URL")
	return ""
}

// skipOrFail decides whether a missing database URL is a skip or a bug.
//
// The first version of this reasoned "CI defines these, so a skip in CI would
// be a false green" and called t.Fatal whenever CI was set. That was wrong and
// CI caught it: the `go` job has CI=true and NO Postgres service, because these
// two tests are the only Go tests in the repository that need one. Only the
// `integration` job has a database.
//
// So the discriminator is DATABASE_URL, not CI. If it is set, we are in the
// integration job and a missing APP_DATABASE_URL means the harness is broken —
// which must fail, or the tenant isolation proof silently stops running. If
// neither is set, there is no database here by design.
func skipOrFail(t *testing.T, name string) {
	t.Helper()
	if os.Getenv("DATABASE_URL") != "" {
		t.Fatalf("%s is unset but DATABASE_URL is set; the integration harness is misconfigured", name)
	}
	t.Skipf("%s unset; these tests need Postgres. Run `make db-setup`, or see the integration CI job", name)
}

func openApp(t *testing.T) *db.Pool {
	t.Helper()
	cfg := db.Defaults()
	cfg.URL = appURL(t)
	cfg.ApplicationName = "db-test"
	pool, err := db.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open as the app role: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestOpenRefusesAPrivilegedRole is the most important test in this package.
//
// Row-level security is inert for a superuser or a role with BYPASSRLS, and
// there is no way to notice from the application's own queries: they simply
// return more rows than they should, with no error. So the pool refuses to
// start rather than serving traffic with no tenant isolation.
func TestOpenRefusesAPrivilegedRole(t *testing.T) {
	t.Parallel()
	cfg := db.Defaults()
	cfg.URL = ownerURL(t)

	pool, err := db.Open(context.Background(), cfg)
	if err == nil {
		pool.Close()
		t.Fatal("Open accepted a superuser role; every RLS policy would be inert")
	}
	if !errors.Is(err, db.ErrPrivilegedRole) {
		t.Fatalf("want ErrPrivilegedRole, got %v", err)
	}
	// The message has to name the fix, because the person reading it is
	// mid-deploy and the symptom is "the service will not start".
	for _, want := range []string{"halyard_app", "tenant isolation"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message should mention %q: %v", want, err)
		}
	}
}

// TestOpenAllowsAPrivilegedRoleWhenAsked covers the migration runner's path.
func TestOpenAllowsAPrivilegedRoleWhenAsked(t *testing.T) {
	t.Parallel()
	cfg := db.Defaults()
	cfg.URL = ownerURL(t)
	cfg.AllowPrivilegedRole = true

	pool, err := db.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open with AllowPrivilegedRole: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestOpenRejectsAnUnknownExecMode(t *testing.T) {
	t.Parallel()
	cfg := db.Defaults()
	cfg.URL = "postgres://x/y"
	cfg.StatementCacheMode = "turbo"
	_, err := db.Open(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "unknown statement cache mode") {
		t.Fatalf("want a mode error, got %v", err)
	}
}

func TestOpenNeverEchoesTheConnectionString(t *testing.T) {
	t.Parallel()
	// The connection string carries the password, and a boot failure is
	// precisely when it would be logged.
	cfg := db.Defaults()
	cfg.URL = "postgres://user:hunter2-correct-horse@nowhere.invalid:5432/db?sslmode=bogus"
	_, err := db.Open(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected a parse failure")
	}
	if strings.Contains(err.Error(), "hunter2-correct-horse") {
		t.Fatalf("the error leaked the password: %v", err)
	}
}

func TestScopeRequiresAnOrg(t *testing.T) {
	t.Parallel()
	pool := openApp(t)
	err := pool.Scope(context.Background(), "", func(context.Context, pgx.Tx) error {
		t.Fatal("fn should not run without an org")
		return nil
	})
	if !errors.Is(err, db.ErrNoOrg) {
		t.Fatalf("want ErrNoOrg, got %v", err)
	}
}

func TestScopeSetsTheOrgForTheTransaction(t *testing.T) {
	t.Parallel()
	pool := openApp(t)
	const org = "11111111-1111-1111-1111-111111111111"

	var seen string
	err := pool.Scope(context.Background(), org, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		seen, err = db.ScopedOrg(ctx, tx)
		return err
	})
	if err != nil {
		t.Fatalf("Scope: %v", err)
	}
	if seen != org {
		t.Fatalf("inside the transaction app.org_id = %q, want %q", seen, org)
	}
}

// TestScopeDoesNotLeakOntoTheConnection is the pooling hazard.
//
// `app.org_id` set outside a transaction persists for the whole session, so it
// would be inherited by whoever borrows that connection next — one tenant
// reading another's data with no code change and no error. Verified against a
// real pool rather than reasoned about.
func TestScopeDoesNotLeakOntoTheConnection(t *testing.T) {
	t.Parallel()
	pool := openApp(t)
	const first = "11111111-1111-1111-1111-111111111111"

	if err := pool.Scope(context.Background(), first, func(ctx context.Context, tx pgx.Tx) error {
		return nil
	}); err != nil {
		t.Fatalf("Scope: %v", err)
	}

	// MinConns is 2 and MaxConns 10, so this is not guaranteed to be the same
	// physical connection. Hammer it: with the pool this small, a leak shows up
	// well within this many acquisitions.
	for i := 0; i < 50; i++ {
		var got string
		err := pool.Unscoped().QueryRow(context.Background(),
			`select coalesce(nullif(current_setting('app.org_id', true), ''), '')`).Scan(&got)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if got != "" {
			t.Fatalf("acquisition %d inherited app.org_id = %q from an earlier transaction", i, got)
		}
	}
}

func TestScopeRollsBackOnError(t *testing.T) {
	t.Parallel()
	pool := openApp(t)
	sentinel := errors.New("no")

	// A real write, so the rollback is observable rather than assumed. The org
	// id is the scope, which is what lets the insert past the policy's check on
	// new rows.
	org := uuid.NewString()
	slug := "rollback-" + org[:8]

	err := pool.Scope(context.Background(), org, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			"insert into orgs (id, name, slug) values ($1, 'Rolled Back', $2)",
			org, slug); err != nil {
			return err
		}
		// Visible inside the transaction...
		var n int
		if err := tx.QueryRow(ctx, "select count(*) from orgs where id = $1", org).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return errors.New("the insert was not visible inside its own transaction")
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Scope should return fn's error unwrapped, got %v", err)
	}

	// ...and gone afterwards. Read back through a fresh scope, because an
	// unscoped read would see nothing either way and prove nothing.
	var after int
	if err := pool.Scope(context.Background(), org, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "select count(*) from orgs where id = $1", org).Scan(&after)
	}); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if after != 0 {
		t.Fatalf("the row survived a failed Scope; %d rows remain", after)
	}
}

func TestScopeReRaisesAPanicAfterRollingBack(t *testing.T) {
	t.Parallel()
	pool := openApp(t)
	const org = "11111111-1111-1111-1111-111111111111"

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("the panic was swallowed; the chassis recovery middleware would never see it")
		}
	}()
	_ = pool.Scope(context.Background(), org, func(ctx context.Context, tx pgx.Tx) error {
		panic("boom")
	})
}

func TestPingIsCheap(t *testing.T) {
	t.Parallel()
	pool := openApp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if s := pool.Stat(); s.MaxConns() != db.Defaults().MaxConns {
		t.Errorf("Stat reports MaxConns %d, want %d", s.MaxConns(), db.Defaults().MaxConns)
	}
}
