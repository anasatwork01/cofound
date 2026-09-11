package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/db"
	"github.com/google/uuid"
)

// Session is a verified console session.
type Session struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	IssuedAt          time.Time
	LastSeenAt        time.Time
	ExpiresAt         time.Time
	AbsoluteExpiresAt time.Time
}

// SessionPolicy is the lifetime of a session.
//
// Three separate numbers because they answer three different questions, and
// collapsing any two of them loses something:
//
//   - IdleTimeout: how long an unused session survives. Bounds the window in
//     which a cookie stolen from a machine the user has walked away from is
//     still worth anything.
//   - AbsoluteTimeout: how long a session survives no matter how much it is
//     used. Without this, "rotating" means "immortal, under a new name each
//     time", which is the opposite of what rotation is for. It is also the only
//     thing that eventually ejects a session an attacker keeps warm.
//   - RotateAfter: how often the token changes. Rotation limits the value of a
//     token captured from a log, a proxy, or a browser extension, because it
//     stops working without the holder doing anything.
type SessionPolicy struct {
	IdleTimeout     time.Duration
	AbsoluteTimeout time.Duration
	RotateAfter     time.Duration

	// RotateGrace keeps a just-rotated token working briefly.
	//
	// This is not slack for its own sake, it is the fix for a specific bug. A
	// console page issues several requests at once. If the first rotates the
	// token, the others are still carrying the old one and would each be
	// rejected — signing the user out for the crime of loading a page. During
	// the grace window the old token is accepted and the caller is handed the
	// successor, so the client catches up on whichever response arrives first.
	RotateGrace time.Duration
}

// DefaultSessionPolicy is what a service gets if it says nothing.
//
// Fourteen days idle, ninety absolute. A builder's console is a tool people
// return to across a fortnight of evenings, and signing them out mid-project is
// a real cost against a speculative one. Ninety days is the outer bound that
// makes "sign out everywhere" mean something on its own eventually.
//
// Daily rotation rather than hourly: hourly would multiply session rows by
// twenty-four for a threat model where the difference between a one-hour and a
// one-day window is not what saves you.
func DefaultSessionPolicy() SessionPolicy {
	return SessionPolicy{
		IdleTimeout:     14 * 24 * time.Hour,
		AbsoluteTimeout: 90 * 24 * time.Hour,
		RotateAfter:     24 * time.Hour,
		RotateGrace:     60 * time.Second,
	}
}

// Sessions issues and verifies console sessions.
type Sessions struct {
	pool   *db.Pool
	policy SessionPolicy
	now    func() time.Time
}

// NewSessions returns a session store.
//
// `now` is injected so the rotation and expiry tests do not sleep. Nil means
// time.Now.
func NewSessions(pool *db.Pool, policy SessionPolicy, now func() time.Time) *Sessions {
	if now == nil {
		now = time.Now
	}
	return &Sessions{pool: pool, policy: policy, now: now}
}

// ErrNoSession means the request carried no usable session.
//
// Deliberately one error for "no cookie", "unknown token", "expired", "revoked"
// and "rotated too long ago". The caller's response is identical in every case,
// and distinguishing them in the returned error invites a handler that reports
// which — turning the endpoint into an oracle for whether a captured token was
// ever real.
var ErrNoSession = errors.New("auth: no valid session")

// Issue creates a session for a user who has just proven their identity.
func (s *Sessions) Issue(ctx context.Context, userID uuid.UUID, ip net.IP, userAgent string) (Token, *Session, error) {
	token, hash, err := NewToken()
	if err != nil {
		return Token{}, nil, err
	}
	now := s.now().UTC()

	sess := &Session{
		UserID:            userID,
		IssuedAt:          now,
		LastSeenAt:        now,
		ExpiresAt:         now.Add(s.policy.IdleTimeout),
		AbsoluteExpiresAt: now.Add(s.policy.AbsoluteTimeout),
	}
	// The idle deadline can never exceed the absolute one; the table has a
	// check constraint saying so, and clamping here means a misconfigured
	// policy is a shorter session rather than a failed sign-in.
	if sess.ExpiresAt.After(sess.AbsoluteExpiresAt) {
		sess.ExpiresAt = sess.AbsoluteExpiresAt
	}

	err = s.pool.Unscoped().QueryRow(ctx, `
		insert into user_sessions
		  (user_id, token_hash, issued_at, last_seen_at, expires_at, absolute_expires_at, ip, user_agent)
		values ($1, $2, $3, $3, $4, $5, $6, $7)
		returning id`,
		userID, []byte(hash), now, sess.ExpiresAt, sess.AbsoluteExpiresAt,
		nullableIP(ip), truncate(userAgent, 512),
	).Scan(&sess.ID)
	if err != nil {
		return Token{}, nil, fmt.Errorf("auth: issue session: %w", err)
	}
	return token, sess, nil
}

// Verify checks a token and returns the session it names.
//
// The second return is non-zero when the session was rotated and the caller
// must set a new cookie. Callers must handle that on EVERY response, not only
// on sign-in, or rotation silently never takes effect.
func (s *Sessions) Verify(ctx context.Context, token Token, ip net.IP, userAgent string) (*Session, Token, error) {
	if token.IsZero() {
		return nil, Token{}, ErrNoSession
	}
	hash := HashToken(token)
	now := s.now().UTC()

	var (
		sess      Session
		revoked   *time.Time
		rotatedAt *time.Time
	)
	err := s.pool.Unscoped().QueryRow(ctx, `
		select id, user_id, issued_at, last_seen_at, expires_at, absolute_expires_at,
		       revoked_at, rotated_at
		  from user_sessions
		 where token_hash = $1`, []byte(hash),
	).Scan(&sess.ID, &sess.UserID, &sess.IssuedAt, &sess.LastSeenAt,
		&sess.ExpiresAt, &sess.AbsoluteExpiresAt, &revoked, &rotatedAt)
	if err != nil {
		if errors.Is(err, db.ErrNoRows) {
			return nil, Token{}, ErrNoSession
		}
		return nil, Token{}, fmt.Errorf("auth: verify session: %w", err)
	}

	switch {
	case revoked != nil:
		return nil, Token{}, ErrNoSession
	case !now.Before(sess.AbsoluteExpiresAt), !now.Before(sess.ExpiresAt):
		return nil, Token{}, ErrNoSession
	case rotatedAt != nil:
		// A token we have already replaced. Inside the grace window this is the
		// concurrent-request case, and the right answer is to hand over the
		// successor rather than to sign the user out.
		if now.Sub(*rotatedAt) > s.policy.RotateGrace {
			return nil, Token{}, ErrNoSession
		}
		return s.successor(ctx, sess.ID)
	}

	if now.Sub(sess.IssuedAt) >= s.policy.RotateAfter {
		return s.rotate(ctx, &sess, ip, userAgent, now)
	}

	// Not rotating: extend the idle deadline, bounded by the absolute one.
	//
	// last_seen_at is written on every verified request, which is a write on
	// every read. That is a deliberate cost: without it the idle timeout cannot
	// exist, and an idle timeout is the only thing that expires a session
	// nobody is using. Task 0.10's rate limiting bounds how often this runs.
	next := now.Add(s.policy.IdleTimeout)
	if next.After(sess.AbsoluteExpiresAt) {
		next = sess.AbsoluteExpiresAt
	}
	if _, err := s.pool.Unscoped().Exec(ctx, `
		update user_sessions set last_seen_at = $2, expires_at = $3 where id = $1`,
		sess.ID, now, next); err != nil {
		return nil, Token{}, fmt.Errorf("auth: touch session: %w", err)
	}
	sess.LastSeenAt = now
	sess.ExpiresAt = next
	return &sess, Token{}, nil
}

// successor returns the session that replaced id, for the grace path.
func (s *Sessions) successor(ctx context.Context, id uuid.UUID) (*Session, Token, error) {
	// No new token is returned, and that is deliberate rather than a gap.
	//
	// The successor's raw token cannot be recovered — only its hash was stored
	// — so this path cannot hand it over. It does not need to: the request that
	// DID the rotation received the new cookie, so the browser already holds it.
	// These are its siblings, and all they need is to succeed rather than to
	// re-learn the cookie. Rotating again here would mint a fresh chain link
	// per concurrent request, which is how a page load turns into six sessions.
	var sess Session
	err := s.pool.Unscoped().QueryRow(ctx, `
		select id, user_id, issued_at, last_seen_at, expires_at, absolute_expires_at
		  from user_sessions
		 where rotated_from = $1 and revoked_at is null
		 order by issued_at desc
		 limit 1`, id,
	).Scan(&sess.ID, &sess.UserID, &sess.IssuedAt, &sess.LastSeenAt,
		&sess.ExpiresAt, &sess.AbsoluteExpiresAt)
	if err != nil {
		if errors.Is(err, db.ErrNoRows) {
			return nil, Token{}, ErrNoSession
		}
		return nil, Token{}, fmt.Errorf("auth: find successor: %w", err)
	}
	return &sess, Token{}, nil
}

// rotate replaces a session's token, keeping the absolute deadline.
func (s *Sessions) rotate(ctx context.Context, old *Session, ip net.IP, userAgent string, now time.Time) (*Session, Token, error) {
	token, hash, err := NewToken()
	if err != nil {
		return nil, Token{}, err
	}

	next := now.Add(s.policy.IdleTimeout)
	if next.After(old.AbsoluteExpiresAt) {
		next = old.AbsoluteExpiresAt
	}

	fresh := &Session{
		UserID:            old.UserID,
		IssuedAt:          now,
		LastSeenAt:        now,
		ExpiresAt:         next,
		AbsoluteExpiresAt: old.AbsoluteExpiresAt, // NOT extended. That is the point.
	}

	// One transaction: the new row must exist before the old one is marked
	// rotated, or a crash between them leaves the user holding a token that
	// points at a successor that does not exist.
	tx, err := s.pool.Unscoped().Begin(ctx)
	if err != nil {
		return nil, Token{}, fmt.Errorf("auth: rotate: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := tx.QueryRow(ctx, `
		insert into user_sessions
		  (user_id, token_hash, issued_at, last_seen_at, expires_at,
		   absolute_expires_at, rotated_from, ip, user_agent)
		values ($1, $2, $3, $3, $4, $5, $6, $7, $8)
		returning id`,
		fresh.UserID, []byte(hash), now, fresh.ExpiresAt, fresh.AbsoluteExpiresAt,
		old.ID, nullableIP(ip), truncate(userAgent, 512),
	).Scan(&fresh.ID); err != nil {
		return nil, Token{}, fmt.Errorf("auth: rotate insert: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`update user_sessions set rotated_at = $2 where id = $1`, old.ID, now); err != nil {
		return nil, Token{}, fmt.Errorf("auth: rotate mark: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, Token{}, fmt.Errorf("auth: rotate commit: %w", err)
	}
	return fresh, token, nil
}

// Revoke ends one session.
func (s *Sessions) Revoke(ctx context.Context, id uuid.UUID, reason string) error {
	_, err := s.pool.Unscoped().Exec(ctx, `
		update user_sessions
		   set revoked_at = $2, revoked_reason = $3
		 where id = $1 and revoked_at is null`, id, s.now().UTC(), truncate(reason, 200))
	if err != nil {
		return fmt.Errorf("auth: revoke: %w", err)
	}
	return nil
}

// RevokeAllForUser is "sign out everywhere".
//
// Includes rotated-but-not-yet-expired rows, because a session chain the user
// wants gone must go entirely — leaving a rotated predecessor inside its grace
// window would keep a stolen cookie alive for another minute, which is exactly
// the minute someone reaching for this button cares about.
func (s *Sessions) RevokeAllForUser(ctx context.Context, userID uuid.UUID, reason string) (int64, error) {
	tag, err := s.pool.Unscoped().Exec(ctx, `
		update user_sessions
		   set revoked_at = $2, revoked_reason = $3
		 where user_id = $1 and revoked_at is null`, userID, s.now().UTC(), truncate(reason, 200))
	if err != nil {
		return 0, fmt.Errorf("auth: revoke all: %w", err)
	}
	return tag.RowsAffected(), nil
}

// Sweep deletes sessions and login tokens that can no longer be used.
//
// Called by a scheduled job, not on a request. Expired rows are already
// unusable — Verify checks the deadlines — so this is housekeeping rather than
// a control, and deleting them is what stops the table growing without bound.
func (s *Sessions) Sweep(ctx context.Context) (sessions, tokens int64, err error) {
	now := s.now().UTC()
	tag, err := s.pool.Unscoped().Exec(ctx,
		`delete from user_sessions where absolute_expires_at < $1`, now)
	if err != nil {
		return 0, 0, fmt.Errorf("auth: sweep sessions: %w", err)
	}
	sessions = tag.RowsAffected()

	tag, err = s.pool.Unscoped().Exec(ctx,
		`delete from login_tokens where expires_at < $1 or consumed_at is not null`, now)
	if err != nil {
		return sessions, 0, fmt.Errorf("auth: sweep login tokens: %w", err)
	}
	return sessions, tag.RowsAffected(), nil
}

// Unauthenticated is the wire error for a missing or invalid session.
func Unauthenticated() *errs.Error { return errs.Unauthenticated() }

func nullableIP(ip net.IP) any {
	if ip == nil {
		return nil
	}
	return ip.String()
}

// truncate bounds a caller-supplied string before it reaches a column.
//
// A user agent is attacker-controlled and unbounded. Postgres would accept
// megabytes of it in a text column, which turns a login into a storage
// amplification.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
