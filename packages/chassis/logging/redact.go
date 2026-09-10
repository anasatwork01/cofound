// Package logging builds the only slog handler a chassis service uses.
//
// SPEC 17.3 requires structured JSON logs with no secret values and no user
// content above debug level. That is enforced here by a wrapping slog.Handler
// rather than by HandlerOptions.ReplaceAttr, for four measured reasons:
// ReplaceAttr cannot see a group's own key, so slog.Group("token", ...) leaks
// verbatim; it receives no level, so it cannot gate user content on Debug; it
// runs after Value.Resolve(), so it cannot detect a marker type; and it is a
// field of the built-in handlers only, so nothing stops a caller building a
// handler without it.
//
// Two residual holes remain, stated rather than papered over:
//   - A credential interpolated into the message string by code we do not own
//     cannot be redacted by key matching. ScanMessage catches known shapes; an
//     unknown provider's format still leaks.
//   - Raising the level to debug deliberately reveals user content. That is a
//     privacy action, not a verbosity tweak.
package logging

import "strings"

// Redacted replaces a secret value.
const Redacted = "[redacted]"

// denySubstrings classify a key as secret-bearing. Substring matching is
// deliberately broad: a new provider's SDK will invent a field name nobody
// predicted, and over-redacting a field is recoverable while leaking a
// credential is not.
var denySubstrings = []string{
	"token", "secret", "password", "passwd", "authorization", "auth",
	"cookie", "ciphertext", "refresh", "credential", "signature",
	"bearer", "apikey", "dek", "key", "dsn",
}

// allowExact rescues keys the denylist would otherwise swallow. Checked first.
// This list is load-bearing: without it "key" alone eats idempotency_key and
// cache_key, and "auth" eats author. A test ties it to logkey.All().
var allowExact = []string{
	"idempotency_key", "cache_key", "event_key", "key_id",
	"public_key", "kms_key_id", "price_book_key", "config_key",
	"session_id", "org_id", "project_id",
	"keyboard", "monkey_patch", "author", "authored_at", "authorized_at",
}

// IsSecretKey reports whether a value logged under key must be redacted.
func IsSecretKey(key string) bool {
	k := strings.ToLower(key)
	for _, a := range allowExact {
		if k == a {
			return false
		}
	}
	for _, d := range denySubstrings {
		if strings.Contains(k, d) {
			return true
		}
	}
	return false
}

// Denylist returns the substrings that mark a key secret.
func Denylist() []string { return append([]string(nil), denySubstrings...) }

// Allowlist returns the exact keys rescued from the denylist.
func Allowlist() []string { return append([]string(nil), allowExact...) }
