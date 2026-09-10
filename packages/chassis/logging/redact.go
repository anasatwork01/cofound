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
//
// The vocabulary this file applies — the denylist, the allowlist and the
// credential prefixes — is generated into vocab.gen.go from
// packages/schema/observability.json, so the Python chassis and task 2.13's
// pre-receive scanner match it by construction rather than by review.
package logging

import "strings"

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
