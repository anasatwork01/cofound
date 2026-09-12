package auth

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/anasatwork01/cofound/packages/chassis/logging"
)

// The link is a live, single-use credential. These two tests are the whole
// contract of the log-only mailer, and they exist because the version before
// them was wrong in a way that looked right: it logged the link as user content
// on an INFO record, and the chassis redactor decides per record
// (`debug := r.Level <= slog.LevelDebug`, logging/guard.go). So the link was
// replaced with the omitted marker at EVERY log level, including debug.
//
// Nothing caught it. The mailer's own doc comment claimed a developer could see
// the link "by choosing to", the code read as though that were true, and there
// was no mailer test at all. With no transactional email provider specified
// (docs/open-questions.md Q5) that was the only local sign-in path, so the
// practical effect was that the application could not be signed into at all on
// a developer's machine.

func logAt(t *testing.T, level slog.Level, link string) string {
	t.Helper()
	var sink bytes.Buffer
	log, _ := logging.New(logging.Config{
		Service: "api",
		Level:   level,
		Format:  logging.FormatText,
		Out:     &sink,
	})
	if err := (LogMailer{Log: log}).SendMagicLink(context.Background(), "dev@example.com", link); err != nil {
		t.Fatalf("SendMagicLink: %v", err)
	}
	return sink.String()
}

func TestAtInfoTheLinkIsNotInTheLogsAtAll(t *testing.T) {
	t.Parallel()
	const link = "http://localhost:3000/auth/verify?token=live-single-use-credential"

	out := logAt(t, slog.LevelInfo, link)

	// The operator still learns that mail was not sent, and to whom.
	if !strings.Contains(out, "no mailer is configured") {
		t.Errorf("info should still report that nothing was sent:\n%s", out)
	}
	if !strings.Contains(out, "dev@example.com") {
		t.Errorf("info should name the address, which is what makes the line useful:\n%s", out)
	}
	// But the credential is absent entirely — not merely redacted. A
	// misconfigured staging box must not put live sign-in links into a log
	// pipeline in any form.
	if strings.Contains(out, "live-single-use-credential") {
		t.Errorf("the magic link leaked at info level:\n%s", out)
	}
}

func TestAtDebugTheDeveloperCanActuallySeeTheLink(t *testing.T) {
	t.Parallel()
	const link = "http://localhost:3000/auth/verify?token=live-single-use-credential"

	out := logAt(t, slog.LevelDebug, link)

	// This is the assertion the old code could never have passed, at any level.
	// SPEC §17.3 treats lowering the level to debug as a deliberate privacy
	// action; the point is that it then actually reveals something.
	if !strings.Contains(out, link) {
		t.Errorf("debug must reveal the link, or there is no way to sign in locally:\n%s", out)
	}
	if strings.Contains(out, logging.OmittedUserContent) {
		t.Errorf("the link is still being redacted at debug:\n%s", out)
	}
}
