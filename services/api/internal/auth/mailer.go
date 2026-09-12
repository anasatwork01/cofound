package auth

import (
	"context"
	"log/slog"

	"github.com/anasatwork01/cofound/packages/chassis/logging"
	"github.com/anasatwork01/cofound/packages/chassis/logkey"
)

// Mailer sends the one email this service sends.
//
// An interface with no vendor behind it yet, deliberately. SPEC §8 requires
// email magic links but neither §3's stack nor §22's verification list names a
// transactional email provider, and working agreement 4 says not to write
// hopeful code against an API nobody has documented. See
// docs/open-questions.md Q5.
//
// The interface is narrow enough that adding a provider later is one type: it
// takes an address and a link, not a template name, a merge dictionary or a
// campaign id, because those are shapes particular providers impose and
// choosing one now would be the guess this avoids.
type Mailer interface {
	SendMagicLink(ctx context.Context, email, link string) error
}

// LogMailer writes the link to the log instead of sending it.
//
// The development default, and the reason sign-in works on a laptop with no
// vendor account. It refuses to run outside development: a production deploy
// that reached this would be printing sign-in links for arbitrary addresses
// into its logs, which is a credential-in-the-log-aggregator incident rather
// than a missing feature.
type LogMailer struct {
	Log *slog.Logger
}

// SendMagicLink says at info that no mail was sent, and emits the link itself
// as a separate DEBUG record.
//
// The two-record shape is load-bearing, and a single info record does not work.
// The chassis redactor decides per RECORD, not per logger:
// `debug := r.Level <= slog.LevelDebug` in logging/guard.go. So user content on
// an info record is replaced with the omitted marker at every LOG_LEVEL,
// including debug — which is what this function used to do, making the link
// unreachable by any means and leaving no way to sign in locally at all. There
// is no other mailer (docs/open-questions.md Q5), so that was the whole
// local-development sign-in path.
//
// Splitting it keeps both properties that mattered:
//
//   - At info, the operator learns mail was not sent and to whom, and the link
//     is not in the record at all — so a misconfigured staging box cannot spray
//     live credentials into a log pipeline even in redacted form.
//   - At debug, the developer who deliberately lowered the level sees the link.
//     SPEC §17.3 treats that as a privacy action, which is the point: you see it
//     by choosing to.
func (m LogMailer) SendMagicLink(ctx context.Context, email, link string) error {
	log := m.Log
	if log == nil {
		log = slog.Default()
	}
	log.InfoContext(ctx, "magic link not sent: no mailer is configured",
		slog.String(logkey.Component, "auth"),
		// The address is not user content in the chassis's sense but it is
		// personal data, and it is the one field that makes the log line useful
		// to the developer who just typed it.
		slog.String("email", email),
	)
	// Debug, so the redactor unwraps it. At info this record is filtered out
	// before the guard ever sees it.
	log.DebugContext(ctx, "magic link",
		slog.String(logkey.Component, "auth"),
		slog.String("email", email),
		logging.User("magic_link", link),
	)
	return nil
}
