package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/db"
	"github.com/google/uuid"
)

// MagicLinkPolicy bounds the magic link flow.
type MagicLinkPolicy struct {
	// TTL is how long a link works. Short, because the link sits in an inbox
	// and an inbox is a place credentials are read from later — by a forwarded
	// thread, a shared family account, or whoever has the laptop.
	TTL time.Duration

	// MaxPerEmail and Window throttle requests for one address.
	//
	// This is not primarily about brute force — the token is 256 bits. It is
	// about not being a free email-bombing service: without it, anyone can
	// point the endpoint at a victim's address and generate as many messages as
	// they like, from our domain, which costs us the domain's reputation.
	MaxPerEmail int
	Window      time.Duration
}

// DefaultMagicLinkPolicy is fifteen minutes and five links per address per hour.
func DefaultMagicLinkPolicy() MagicLinkPolicy {
	return MagicLinkPolicy{TTL: 15 * time.Minute, MaxPerEmail: 5, Window: time.Hour}
}

// MagicLinks implements passwordless sign-in by email.
type MagicLinks struct {
	pool   *db.Pool
	mailer Mailer
	policy MagicLinkPolicy
	// BaseURL is the console origin the link points at. The link is built from
	// configuration, never from the request's Host header: an attacker who can
	// set Host could otherwise have us email a link pointing at their server,
	// and the user would hand over a valid token by clicking it.
	baseURL string
	now     func() time.Time
}

// NewMagicLinks returns the flow.
func NewMagicLinks(pool *db.Pool, mailer Mailer, policy MagicLinkPolicy, baseURL string, now func() time.Time) *MagicLinks {
	if now == nil {
		now = time.Now
	}
	return &MagicLinks{pool: pool, mailer: mailer, policy: policy, baseURL: baseURL, now: now}
}

// ErrThrottled means this address has requested too many links.
var ErrThrottled = errors.New("auth: too many magic links requested for this address")

// NormaliseEmail lowercases and trims an address.
//
// Only case and whitespace. Deliberately NOT the popular extras — stripping
// dots or +suffixes from Gmail addresses — because those rules are
// provider-specific, change without notice, and applying them means two
// distinct addresses at a provider that does not share our assumptions collapse
// into one account. Under-normalising creates a duplicate account, which is
// recoverable; over-normalising hands one person's account to another.
func NormaliseEmail(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", errs.InvalidField("email", "must be a valid address")
	}
	// ParseAddress accepts `Name <a@b>`; only the address part is wanted.
	at := strings.LastIndex(addr.Address, "@")
	if at <= 0 || at == len(addr.Address)-1 {
		return "", errs.InvalidField("email", "must be a valid address")
	}
	// The local part is case-SENSITIVE per RFC 5321 and almost universally
	// treated as insensitive in practice. Lowercasing it is the pragmatic
	// choice and is noted because it is a deviation from the standard.
	return strings.ToLower(addr.Address), nil
}

// Request creates a magic link and sends it.
//
// Returns no indication of whether the address has an account. That is the
// whole reason the handler answers 202 unconditionally: a different response
// for a known address turns this endpoint into a way to test whether someone
// has one, which for a product people build businesses on is information worth
// having.
func (m *MagicLinks) Request(ctx context.Context, email string, ip net.IP, userAgent string) error {
	normalised, err := NormaliseEmail(email)
	if err != nil {
		return err
	}
	now := m.now().UTC()

	var recent int
	if err := m.pool.Unscoped().QueryRow(ctx, `
		select count(*) from login_tokens
		 where email = $1 and created_at > $2`,
		normalised, now.Add(-m.policy.Window)).Scan(&recent); err != nil {
		return fmt.Errorf("auth: throttle check: %w", err)
	}
	if recent >= m.policy.MaxPerEmail {
		return ErrThrottled
	}

	token, hash, err := NewToken()
	if err != nil {
		return err
	}
	if _, err := m.pool.Unscoped().Exec(ctx, `
		insert into login_tokens
		  (email, token_hash, expires_at, requested_ip, requested_user_agent)
		values ($1, $2, $3, $4, $5)`,
		normalised, []byte(hash), now.Add(m.policy.TTL),
		nullableIP(ip), truncate(userAgent, 512)); err != nil {
		return fmt.Errorf("auth: store login token: %w", err)
	}

	link, err := m.link(token)
	if err != nil {
		return err
	}
	if err := m.mailer.SendMagicLink(ctx, normalised, link); err != nil {
		// The token row stays. It expires on its own, and deleting it here
		// would mean a transient mailer failure also burned the user's throttle
		// budget in the other direction — they would retry and succeed, which
		// is the behaviour wanted.
		return fmt.Errorf("auth: send magic link: %w", err)
	}
	return nil
}

func (m *MagicLinks) link(token Token) (string, error) {
	base, err := url.Parse(m.baseURL)
	if err != nil || base.Host == "" {
		return "", fmt.Errorf("auth: CONSOLE_ORIGIN is not a usable URL")
	}
	base.Path = "/auth/callback"
	base.RawQuery = url.Values{"token": {token.Secret()}}.Encode()
	return base.String(), nil
}

// Consume verifies a link token and returns the user it authenticates.
//
// Creates the user on first sign-in: the address has just been proven, which is
// the only moment at which creating it is safe. Doing it at Request time would
// let anyone create an account for any address they can spell.
func (m *MagicLinks) Consume(ctx context.Context, token Token) (uuid.UUID, string, error) {
	if token.IsZero() {
		return uuid.Nil, "", ErrNoSession
	}
	hash := HashToken(token)
	now := m.now().UTC()

	tx, err := m.pool.Unscoped().Begin(ctx)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("auth: consume: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// Consume and read in ONE statement. A select-then-update would let two
	// concurrent clicks on the same link both pass the select — the classic
	// double-spend — and single-use is the property that makes a link in an
	// inbox acceptable at all. The `consumed_at is null` predicate in the
	// update is what makes the second click find no row.
	var email string
	err = tx.QueryRow(ctx, `
		update login_tokens
		   set consumed_at = $2
		 where token_hash = $1
		   and consumed_at is null
		   and expires_at > $2
		returning email`, []byte(hash), now).Scan(&email)
	if err != nil {
		if errors.Is(err, db.ErrNoRows) {
			// Unknown, already used, or expired. One error for all three, so
			// the endpoint cannot be used to learn which.
			return uuid.Nil, "", ErrNoSession
		}
		return uuid.Nil, "", fmt.Errorf("auth: consume login token: %w", err)
	}

	var userID uuid.UUID
	err = tx.QueryRow(ctx, `
		insert into users (email) values ($1)
		on conflict (email) do update set email = excluded.email
		returning id`, email).Scan(&userID)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("auth: upsert user: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, "", fmt.Errorf("auth: consume commit: %w", err)
	}
	return userID, email, nil
}
