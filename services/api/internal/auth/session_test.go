package auth

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/anasatwork01/cofound/packages/db"
	"github.com/google/uuid"
)

// These need a real database: rotation, expiry and single-use all live in SQL,
// and a fake store would be testing the fake. The `go` CI job has no Postgres,
// so they skip there and run in the integration job — see packages/db/db_test.go
// for why DATABASE_URL rather than CI is the discriminator.
func testPool(t *testing.T) *db.Pool {
	t.Helper()
	url := os.Getenv("APP_DATABASE_URL")
	if url == "" {
		if os.Getenv("DATABASE_URL") != "" {
			t.Fatal("APP_DATABASE_URL is unset but DATABASE_URL is set; the integration harness is misconfigured")
		}
		t.Skip("APP_DATABASE_URL unset; run `make db-setup`")
	}
	cfg := db.Defaults()
	cfg.URL = url
	cfg.ApplicationName = "auth-test"
	pool, err := db.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// clock is a manually advanced time source, so the rotation and expiry tests
// do not sleep for a day.
type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

func testUser(t *testing.T, pool *db.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	email := "auth-test-" + uuid.NewString() + "@test.invalid"
	err := pool.Unscoped().QueryRow(context.Background(),
		`insert into users (email) values ($1) returning id`, email).Scan(&id)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Unscoped().Exec(context.Background(), `delete from users where id = $1`, id)
	})
	return id
}

func shortPolicy() SessionPolicy {
	return SessionPolicy{
		IdleTimeout:     time.Hour,
		AbsoluteTimeout: 24 * time.Hour,
		RotateAfter:     30 * time.Minute,
		RotateGrace:     time.Minute,
	}
}

func TestIssueThenVerify(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	clk := &clock{t: time.Now().UTC()}
	s := NewSessions(pool, shortPolicy(), clk.now)
	user := testUser(t, pool)
	ctx := context.Background()

	token, issued, err := s.Issue(ctx, user, net.ParseIP("203.0.113.7"), "test-agent")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	got, rotated, err := s.Verify(ctx, token, nil, "")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.ID != issued.ID {
		t.Errorf("Verify returned session %s, want %s", got.ID, issued.ID)
	}
	if !rotated.IsZero() {
		t.Error("a fresh session should not rotate immediately")
	}
}

func TestVerifyRejectsAnUnknownToken(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	s := NewSessions(pool, shortPolicy(), nil)
	stranger, _, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Verify(context.Background(), stranger, nil, ""); !errors.Is(err, ErrNoSession) {
		t.Fatalf("want ErrNoSession, got %v", err)
	}
}

// TestRotationDoesNotExtendTheAbsoluteDeadline is the point of having two.
//
// Without it, "rotating" means "immortal under a new name each time", which is
// the opposite of what rotation is for — and the only thing that eventually
// ejects a session an attacker keeps warm.
func TestRotationDoesNotExtendTheAbsoluteDeadline(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	clk := &clock{t: time.Now().UTC()}
	policy := shortPolicy()
	s := NewSessions(pool, policy, clk.now)
	user := testUser(t, pool)
	ctx := context.Background()

	token, issued, err := s.Issue(ctx, user, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	absolute := issued.AbsoluteExpiresAt

	// Rotate repeatedly, keeping the session in constant use.
	current := token
	for i := range 5 {
		clk.add(policy.RotateAfter + time.Second)
		sess, fresh, err := s.Verify(ctx, current, nil, "")
		if err != nil {
			t.Fatalf("rotation %d: %v", i, err)
		}
		if fresh.IsZero() {
			t.Fatalf("rotation %d: expected a new token past RotateAfter", i)
		}
		if !sess.AbsoluteExpiresAt.Equal(absolute) {
			t.Fatalf("rotation %d moved the absolute deadline from %s to %s",
				i, absolute, sess.AbsoluteExpiresAt)
		}
		current = fresh
	}

	// Past the absolute deadline the session is over, however much it was used.
	clk.t = absolute.Add(time.Second)
	if _, _, err := s.Verify(ctx, current, nil, ""); !errors.Is(err, ErrNoSession) {
		t.Fatalf("the session outlived its absolute deadline: %v", err)
	}
}

// TestConcurrentRequestsSurviveRotation is the bug RotateGrace exists for.
//
// A console page issues several requests at once. If the first rotates the
// token, the others are still carrying the old one — and without a grace window
// each would be rejected, signing the user out for the crime of loading a page.
func TestConcurrentRequestsSurviveRotation(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	clk := &clock{t: time.Now().UTC()}
	policy := shortPolicy()
	s := NewSessions(pool, policy, clk.now)
	user := testUser(t, pool)
	ctx := context.Background()

	token, _, err := s.Issue(ctx, user, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	clk.add(policy.RotateAfter + time.Second)

	// The first request rotates.
	if _, fresh, err := s.Verify(ctx, token, nil, ""); err != nil || fresh.IsZero() {
		t.Fatalf("first request: err=%v rotated=%v", err, !fresh.IsZero())
	}
	// A second request, still carrying the old cookie, must succeed.
	sess, _, err := s.Verify(ctx, token, nil, "")
	if err != nil {
		t.Fatalf("a concurrent request with the pre-rotation token was rejected: %v", err)
	}
	if sess.UserID != user {
		t.Errorf("it resolved to the wrong user")
	}

	// Past the grace window the old token is finished, so a cookie captured
	// earlier does not keep working.
	clk.add(policy.RotateGrace + time.Second)
	if _, _, err := s.Verify(ctx, token, nil, ""); !errors.Is(err, ErrNoSession) {
		t.Fatalf("the rotated token still worked past the grace window: %v", err)
	}
}

func TestIdleTimeoutEndsAnUnusedSession(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	clk := &clock{t: time.Now().UTC()}
	policy := shortPolicy()
	s := NewSessions(pool, policy, clk.now)
	user := testUser(t, pool)
	ctx := context.Background()

	token, _, err := s.Issue(ctx, user, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	clk.add(policy.IdleTimeout + time.Second)
	if _, _, err := s.Verify(ctx, token, nil, ""); !errors.Is(err, ErrNoSession) {
		t.Fatalf("an idle session survived its timeout: %v", err)
	}
}

func TestUsingASessionExtendsTheIdleWindow(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	clk := &clock{t: time.Now().UTC()}
	// Rotation far enough away that this test only exercises the idle path.
	policy := SessionPolicy{
		IdleTimeout:     time.Hour,
		AbsoluteTimeout: 30 * 24 * time.Hour,
		RotateAfter:     29 * 24 * time.Hour,
		RotateGrace:     time.Minute,
	}
	s := NewSessions(pool, policy, clk.now)
	user := testUser(t, pool)
	ctx := context.Background()

	token, _, err := s.Issue(ctx, user, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	// Used every 59 minutes for a day. Total elapsed is far past a single idle
	// window, so without extension this fails on the second iteration.
	for i := range 24 {
		clk.add(policy.IdleTimeout - time.Minute)
		sess, fresh, err := s.Verify(ctx, token, nil, "")
		if err != nil {
			t.Fatalf("use %d after %s elapsed: %v", i, clk.t.Sub(sess2Time(sess)), err)
		}
		if !fresh.IsZero() {
			t.Fatalf("use %d rotated, but RotateAfter is far away", i)
		}
	}

	// And it still ends once actually left alone.
	clk.add(policy.IdleTimeout + time.Second)
	if _, _, err := s.Verify(ctx, token, nil, ""); !errors.Is(err, ErrNoSession) {
		t.Fatalf("the session survived going idle: %v", err)
	}
}

func sess2Time(s *Session) time.Time {
	if s == nil {
		return time.Time{}
	}
	return s.IssuedAt
}

func TestRevokeEndsASessionImmediately(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	s := NewSessions(pool, shortPolicy(), nil)
	user := testUser(t, pool)
	ctx := context.Background()

	token, sess, err := s.Issue(ctx, user, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke(ctx, sess.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Verify(ctx, token, nil, ""); !errors.Is(err, ErrNoSession) {
		t.Fatalf("a revoked session still verified: %v", err)
	}
}

// TestRevokeAllIncludesRotatedPredecessors.
//
// "Sign out everywhere" must end the whole chain. Leaving a rotated
// predecessor alive inside its grace window keeps a stolen cookie working for
// another minute — which is exactly the minute someone reaching for that
// button cares about.
func TestRevokeAllIncludesRotatedPredecessors(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	clk := &clock{t: time.Now().UTC()}
	policy := shortPolicy()
	s := NewSessions(pool, policy, clk.now)
	user := testUser(t, pool)
	ctx := context.Background()

	old, _, err := s.Issue(ctx, user, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	clk.add(policy.RotateAfter + time.Second)
	_, fresh, err := s.Verify(ctx, old, nil, "")
	if err != nil || fresh.IsZero() {
		t.Fatalf("rotate: err=%v", err)
	}

	n, err := s.RevokeAllForUser(ctx, user, "signed out everywhere")
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Errorf("revoked %d sessions, expected the rotated predecessor too", n)
	}
	for name, tok := range map[string]Token{"predecessor": old, "successor": fresh} {
		if _, _, err := s.Verify(ctx, tok, nil, ""); !errors.Is(err, ErrNoSession) {
			t.Errorf("%s still verified after revoke-all: %v", name, err)
		}
	}
}

// TestOnlyHashesAreStored. A database dump must not yield a usable session.
func TestOnlyHashesAreStored(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	s := NewSessions(pool, shortPolicy(), nil)
	user := testUser(t, pool)
	ctx := context.Background()

	token, sess, err := s.Issue(ctx, user, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err := pool.Unscoped().QueryRow(ctx,
		`select token_hash from user_sessions where id = $1`, sess.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(stored) == token.Secret() {
		t.Fatal("the raw token was stored")
	}
	if !Hash(stored).Equal(HashToken(token)) {
		t.Fatal("the stored value is not the token's hash")
	}
	// And no column anywhere holds the raw token.
	var hits int
	if err := pool.Unscoped().QueryRow(ctx,
		`select count(*) from user_sessions where token_hash::text like '%' || $1 || '%'`,
		token.Secret()).Scan(&hits); err != nil {
		t.Fatal(err)
	}
	if hits != 0 {
		t.Fatal("the raw token is recoverable from the table")
	}
}

func TestSweepRemovesOnlyDeadRows(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	s := NewSessions(pool, shortPolicy(), nil)
	user := testUser(t, pool)
	ctx := context.Background()

	live, _, err := s.Issue(ctx, user, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	// A session that is ALREADY dead, written directly rather than by advancing
	// a clock and sweeping.
	//
	// The first version of this test advanced its own clock a day and called
	// Sweep, which deletes every expired row in the table — including rows
	// belonging to tests running in parallel, whose sessions then vanished
	// mid-test. Sweep is global by design (it is housekeeping for the whole
	// table), so a test of it must not rely on a cutoff that reaches other
	// tests' data. Sweeping at `now` only ever removes rows that were already
	// unusable, which is the real behaviour and touches nobody else.
	past := time.Now().UTC().Add(-48 * time.Hour)
	var deadID uuid.UUID
	if err := pool.Unscoped().QueryRow(ctx, `
		insert into user_sessions
		  (user_id, token_hash, issued_at, last_seen_at, expires_at, absolute_expires_at)
		values ($1, $2, $3, $3, $4, $4)
		returning id`,
		user, []byte("dead-"+uuid.NewString()), past,
		past.Add(time.Hour)).Scan(&deadID); err != nil {
		t.Fatalf("seed dead session: %v", err)
	}

	sessions, _, err := s.Sweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sessions < 1 {
		t.Error("Sweep removed nothing, but a dead session was present")
	}

	var deadRemains int
	if err := pool.Unscoped().QueryRow(ctx,
		`select count(*) from user_sessions where id = $1`, deadID).Scan(&deadRemains); err != nil {
		t.Fatal(err)
	}
	if deadRemains != 0 {
		t.Error("the dead session survived the sweep")
	}
	if _, _, err := s.Verify(ctx, live, nil, ""); err != nil {
		t.Fatalf("Sweep removed a live session: %v", err)
	}
}
