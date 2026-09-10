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

// SendMagicLink logs the link at info.
//
// The link contains a live single-use credential, so it is marked user content:
// the chassis redactor replaces it above debug level, and SPEC §17.3 makes
// raising the level to debug a deliberate privacy action. So a developer sees
// the link by choosing to, and a misconfigured staging box does not spray them
// into a log pipeline.
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
		logging.User("magic_link", link),
	)
	return nil
}
