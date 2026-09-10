package logging

import "strings"

// secretPrefixes are literal credential shapes. This closes the one hole key
// matching cannot: a token interpolated into a message by a library we do not
// own, as in log.Printf("auth failed for token=%s", tok).
//
// Literal prefixes only — no regular expressions. Measured at ~172ns with zero
// allocations on a clean string, which is cheap enough to run on every line.
//
// Task 2.13 builds the gitd pre-receive secret scanner and needs the same list.
// Share this one; two copies will diverge, and the copy that goes stale will be
// the one guarding the more important boundary.
var secretPrefixes = []string{
	"sk_live_", "sk_test_", "whsec_", "rk_live_", // Stripe
	"ghp_", "gho_", "github_pat_", // GitHub
	"xoxb-", "xoxp-", // Slack
	"AKIA", "ASIA", // AWS
	"sk-ant-",       // Anthropic
	"AIza", "ya29.", // Google
	"glpat-",      // GitLab
	"eyJhbGciOi",  // a JWT header, base64
	"-----BEGIN ", // a PEM private key block
	"postgres://", "postgresql://",
	"redis://", "rediss://",
	"mysql://", "amqp://",
}

// SuppressedMessage replaces a log message that contained a credential shape.
// The line is still emitted — dropping it entirely would hide the fact that
// something tried to log a secret, which is itself worth knowing.
const SuppressedMessage = "log line suppressed: it contained a credential-shaped literal"

// SecretPrefixes returns the literal credential prefixes.
func SecretPrefixes() []string { return append([]string(nil), secretPrefixes...) }

// ScanMessage reports the first credential shape found in msg.
func ScanMessage(msg string) (pattern string, hit bool) {
	for _, p := range secretPrefixes {
		if strings.Contains(msg, p) {
			return p, true
		}
	}
	return "", false
}
