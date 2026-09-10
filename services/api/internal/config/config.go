// Package config binds the api service's own configuration.
package config

import (
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/config"
)

// Keys the api service reads.
const (
	KeyDatabaseURL   = "DATABASE_URL"
	KeyRedisURL      = "REDIS_URL"
	KeyConsoleOrigin = "CONSOLE_ORIGIN"

	// Google sign-in (SPEC §8). All three or none: a half-configured client
	// produces a redirect to Google that fails at the consent screen, which is
	// worse than the button not being there.
	KeyGoogleClientID     = "GOOGLE_CLIENT_ID"
	KeyGoogleClientSecret = "GOOGLE_CLIENT_SECRET"
	KeyGoogleRedirectURL  = "GOOGLE_REDIRECT_URL"

	KeySessionIdleTimeout     = "SESSION_IDLE_TIMEOUT"
	KeySessionAbsoluteTimeout = "SESSION_ABSOLUTE_TIMEOUT"
	KeySessionRotateAfter     = "SESSION_ROTATE_AFTER"
)

// API is the service's own configuration.
type API struct {
	DatabaseURL   string
	RedisURL      string
	ConsoleOrigin string

	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string

	SessionIdleTimeout     time.Duration
	SessionAbsoluteTimeout time.Duration
	SessionRotateAfter     time.Duration
}

// Binder binds and holds one service's configuration.
//
// This is a value the caller owns, NOT a package-level var. A global here is
// process-wide mutable state: it makes two parallel tests in one binary
// overwrite each other's configuration, which is precisely the failure that
// caught this — TestReadyzIs503WhenAProbeIsDown saw another test's
// DATABASE_URL and reported ready. The same reasoning is why errs.Catalog is
// constructed rather than registered at init.
type Binder struct{ cfg API }

// NewBinder returns an unbound Binder.
func NewBinder() *Binder { return &Binder{} }

// Bind binds api's fields on the SHARED chassis loader.
//
// Sharing the loader is the point: a deploy missing both DATABASE_URL and
// REDIS_URL reports both in one boot rather than one per deploy cycle. Tasks
// 0.6 and 0.7 extend this method.
func (b *Binder) Bind(l *config.Loader) {
	// Required as of task 0.6: the service cannot serve a single endpoint
	// without the control plane database, so starting without it and reporting
	// unready forever is worse than refusing to start with a message that names
	// the variable.
	//
	// It must be the UNPRIVILEGED role's connection string. Row-level security
	// is inert for a superuser or a role with BYPASSRLS, so a deploy that reuses
	// the migration credentials has no tenant isolation -- silently. db.Open
	// refuses such a role rather than trusting this comment.
	if u := l.SecretURL(KeyDatabaseURL, true, "postgres", "postgresql"); u != nil {
		b.cfg.DatabaseURL = u.String()
	}
	if u := l.SecretURL(KeyRedisURL, false, "redis", "rediss"); u != nil {
		b.cfg.RedisURL = u.String()
	}
	// Required as of task 0.7. Every magic link and every OAuth redirect is
	// built from this rather than from the request's Host header: a Host an
	// attacker controls would otherwise let them have us email a link pointing
	// at their own server, and the user would hand over a live token by
	// clicking it. So it cannot be defaulted, and it cannot be optional.
	if u := l.URL(KeyConsoleOrigin, "", "http", "https"); u != nil {
		b.cfg.ConsoleOrigin = u.String()
	} else {
		l.Problemf(KeyConsoleOrigin, "is required: every sign-in link is built from it, never from the request's Host header")
	}

	b.cfg.GoogleClientID = l.String(KeyGoogleClientID, "")
	// Marked before it is read, so the boot line fingerprints it rather than
	// printing it. Order does not matter to the loader -- the set is consulted
	// at render time -- but marking beside the read keeps the two together.
	l.MarkSecret(KeyGoogleClientSecret)
	b.cfg.GoogleClientSecret = l.String(KeyGoogleClientSecret, "")
	if u := l.URL(KeyGoogleRedirectURL, "", "http", "https"); u != nil {
		b.cfg.GoogleRedirectURL = u.String()
	}
	// All three or none. A client id with no secret is a sign-in button that
	// always fails, and the operator who set one of the three deserves to be
	// told rather than to discover it from a user report.
	set := 0
	for _, v := range []string{b.cfg.GoogleClientID, b.cfg.GoogleClientSecret, b.cfg.GoogleRedirectURL} {
		if v != "" {
			set++
		}
	}
	if set != 0 && set != 3 {
		l.Problemf("", "expect all of %s, %s and %s, or none of them",
			KeyGoogleClientID, KeyGoogleClientSecret, KeyGoogleRedirectURL)
	}

	// Bounds rather than defaults-only. A one-second idle timeout signs
	// everyone out constantly and a one-year absolute timeout is not a session,
	// and both are the kind of value a copied env file arrives with.
	b.cfg.SessionIdleTimeout = l.Duration(KeySessionIdleTimeout,
		14*24*time.Hour, time.Minute, 90*24*time.Hour)
	b.cfg.SessionAbsoluteTimeout = l.Duration(KeySessionAbsoluteTimeout,
		90*24*time.Hour, time.Hour, 365*24*time.Hour)
	b.cfg.SessionRotateAfter = l.Duration(KeySessionRotateAfter,
		24*time.Hour, time.Minute, 30*24*time.Hour)
	if b.cfg.SessionIdleTimeout > b.cfg.SessionAbsoluteTimeout {
		l.Problemf(KeySessionIdleTimeout, "expect a value at or below %s (%s); an idle window longer than the absolute one can never be reached",
			KeySessionAbsoluteTimeout, b.cfg.SessionAbsoluteTimeout)
	}
	if b.cfg.SessionRotateAfter > b.cfg.SessionIdleTimeout {
		l.Problemf(KeySessionRotateAfter, "expect a value at or below %s (%s); a session that expires before it rotates never rotates",
			KeySessionIdleTimeout, b.cfg.SessionIdleTimeout)
	}
}

// Config returns the bound configuration.
func (b *Binder) Config() API { return b.cfg }
