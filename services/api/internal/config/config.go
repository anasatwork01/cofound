// Package config binds the api service's own configuration.
package config

import "github.com/anasatwork01/cofound/packages/chassis/config"

// Keys the api service reads.
const (
	KeyDatabaseURL   = "DATABASE_URL"
	KeyRedisURL      = "REDIS_URL"
	KeyConsoleOrigin = "CONSOLE_ORIGIN"
)

// API is the service's own configuration.
type API struct {
	DatabaseURL   string
	RedisURL      string
	ConsoleOrigin string
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
	// Not required yet — task 0.6 introduces the pool and makes it so. Marked
	// secret now, because the fingerprinting must be in place before the value
	// ever exists.
	if u := l.SecretURL(KeyDatabaseURL, false, "postgres", "postgresql"); u != nil {
		b.cfg.DatabaseURL = u.String()
	}
	if u := l.SecretURL(KeyRedisURL, false, "redis", "rediss"); u != nil {
		b.cfg.RedisURL = u.String()
	}
	b.cfg.ConsoleOrigin = l.String(KeyConsoleOrigin, "")
}

// Config returns the bound configuration.
func (b *Binder) Config() API { return b.cfg }
