package logging_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"testing/slogtest"

	"github.com/anasatwork01/cofound/packages/chassis/logging"
	"github.com/anasatwork01/cofound/packages/chassis/logkey"
	"github.com/anasatwork01/cofound/packages/chassis/scope"
)

const secret = "sk_live_51H8xQ2abcdefghijklmnop"

func newLogger(t *testing.T, level slog.Level) (*slog.Logger, *logging.Sink) {
	t.Helper()
	sink := logging.NewSink()
	l, _ := logging.New(logging.Config{
		Service: "api", Env: "test", Level: level, Format: logging.FormatJSON, Out: sink,
	})
	return l, sink
}

// TestGuardSatisfiesSlogtest proves the Guard is a conforming slog.Handler.
//
// Without this, a handler that redacts perfectly could still mishandle groups,
// empty attributes or WithAttrs ordering — and every downstream assertion in
// this file would be testing a broken handler against itself.
func TestGuardSatisfiesSlogtest(t *testing.T) {
	t.Parallel()
	sink := logging.NewSink()
	h, _ := logging.NewHandler(logging.Config{Level: slog.LevelDebug, Format: logging.FormatJSON, Out: sink})

	results := func() []map[string]any {
		var out []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(sink.String()), "\n") {
			if line == "" {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("emitted a line that is not JSON: %v", err)
			}
			// Nested maps, mirroring the JSON output: slogtest looks for a
			// group by its own key, so flattening to dotted keys hides it.
			out = append(out, m)
		}
		return out
	}
	if err := slogtest.TestHandler(h, results); err != nil {
		t.Fatalf("slogtest: %v", err)
	}
}

// TestRedactsOnEveryPath covers the four ways an attribute can reach a handler.
// A guard that only cleans direct attributes leaks the moment someone builds a
// logger with With().
func TestRedactsOnEveryPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name string
		emit func(l *slog.Logger)
	}{
		{"direct", func(l *slog.Logger) { l.InfoContext(ctx, "m", "api_token", secret) }},
		{"with", func(l *slog.Logger) { l.With("api_token", secret).InfoContext(ctx, "m") }},
		{"group_member", func(l *slog.Logger) {
			l.InfoContext(ctx, "m", slog.Group("stripe", "secret_key", secret))
		}},
		{"withgroup", func(l *slog.Logger) {
			l.WithGroup("stripe").InfoContext(ctx, "m", "secret_key", secret)
		}},
		{"nested_group", func(l *slog.Logger) {
			l.InfoContext(ctx, "m", slog.Group("a", slog.Group("b", "password", secret)))
		}},
		{
			// The case ReplaceAttr provably cannot reach: the GROUP's own key is
			// the secret-shaped one, and its members have innocent names.
			"group_key_is_secret",
			func(l *slog.Logger) { l.InfoContext(ctx, "m", slog.Group("token", "value", secret)) },
		},
		{"withgroup_key_is_secret", func(l *slog.Logger) {
			l.WithGroup("authorization").InfoContext(ctx, "m", "value", secret)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l, sink := newLogger(t, slog.LevelDebug)
			tc.emit(l)
			if sink.Contains(secret) {
				t.Fatalf("secret reached the output:\n%s", sink.String())
			}
			if !sink.Contains(logging.Redacted) {
				t.Fatalf("expected %q in the output:\n%s", logging.Redacted, sink.String())
			}
		})
	}
}

// TestAllowlistRescuesTheLogVocabulary ties the allowlist to logkey.All().
//
// The denylist matches substrings, so "key" alone would swallow
// idempotency_key and "auth" would swallow author. Without this test the
// allowlist rots silently and a debugger loses fields with no error anywhere.
func TestAllowlistRescuesTheLogVocabulary(t *testing.T) {
	t.Parallel()
	for _, k := range logkey.All() {
		if logging.IsSecretKey(k) {
			t.Errorf("logkey.%s (%q) is classified secret; add it to the allowlist", k, k)
		}
	}
	for _, k := range []string{"idempotency_key", "cache_key", "event_key", "author", "keyboard", "monkey_patch"} {
		if logging.IsSecretKey(k) {
			t.Errorf("%q must not be classified secret", k)
		}
	}
	for _, k := range []string{"api_token", "STRIPE_SECRET_KEY", "Authorization", "refresh_ciphertext", "database_dsn", "cookie"} {
		if !logging.IsSecretKey(k) {
			t.Errorf("%q must be classified secret", k)
		}
	}
}

// TestUserContentHiddenAboveDebug is the SPEC 17.3 rule: no user content above
// debug level.
func TestUserContentHiddenAboveDebug(t *testing.T) {
	t.Parallel()
	const prompt = "build me a pricing page for my dog grooming business"

	l, sink := newLogger(t, slog.LevelInfo)
	l.InfoContext(context.Background(), "turn started", logging.Prompt(prompt))
	if sink.Contains(prompt) {
		t.Fatalf("user content leaked at info level:\n%s", sink.String())
	}
	if !sink.Contains(logging.OmittedUserContent) {
		t.Fatalf("expected the omitted marker:\n%s", sink.String())
	}

	dl, dsink := newLogger(t, slog.LevelDebug)
	dl.DebugContext(context.Background(), "turn started", logging.Prompt(prompt))
	if !dsink.Contains(prompt) {
		t.Fatalf("debug level must reveal user content, for support:\n%s", dsink.String())
	}
}

// TestUserContentFailsClosedThroughAnUnawareHandler is why User uses
// slog.LogValuer rather than a type the Guard has to recognise.
//
// A marker that only redacted inside the Guard would leak the first time
// someone logged through a handler the chassis did not build.
func TestUserContentFailsClosedThroughAnUnawareHandler(t *testing.T) {
	t.Parallel()
	const prompt = "delete production please"
	sink := logging.NewSink()
	plain := slog.New(slog.NewJSONHandler(sink, nil)) // no Guard anywhere
	plain.Info("turn", logging.Prompt(prompt))
	if sink.Contains(prompt) {
		t.Fatalf("user content must fail closed even without the Guard:\n%s", sink.String())
	}
}

// TestMessageTripwireSuppressesAnInterpolatedCredential covers the hole key
// matching cannot close: a token formatted into the message by code we do not
// own.
func TestMessageTripwireSuppressesAnInterpolatedCredential(t *testing.T) {
	t.Parallel()
	l, sink := newLogger(t, slog.LevelInfo)
	l.InfoContext(context.Background(), "auth failed for token="+secret)

	if sink.Contains(secret) {
		t.Fatalf("credential in the message reached the output:\n%s", sink.String())
	}
	last := sink.Last()
	if last["msg"] != logging.SuppressedMessage {
		t.Fatalf("msg = %v, want the suppression notice", last["msg"])
	}
	// Naming the pattern rather than the value tells an operator which
	// integration to go and fix.
	if last["suppressed_pattern"] != "sk_live_" {
		t.Fatalf("suppressed_pattern = %v, want sk_live_", last["suppressed_pattern"])
	}
}

// TestCorrelationComesFromTheContext proves a call site does not have to
// remember request_id or the tenancy tuple.
func TestCorrelationComesFromTheContext(t *testing.T) {
	t.Parallel()
	ctx := scope.WithRequestID(context.Background(), "REQ123456")
	ctx = scope.With(ctx, scope.Scope{OrgID: "org_1", ProjectID: "proj_2", SessionID: "sess_3", Turn: 7})

	l, sink := newLogger(t, slog.LevelInfo)
	l.InfoContext(ctx, "hello")

	last := sink.Last()
	for k, want := range map[string]any{
		logkey.RequestID: "REQ123456",
		logkey.OrgID:     "org_1",
		logkey.ProjectID: "proj_2",
		logkey.SessionID: "sess_3",
		logkey.Turn:      float64(7),
	} {
		if last[k] != want {
			t.Errorf("%s = %v (%T), want %v", k, last[k], last[k], want)
		}
	}
}

// TestProcessIdentityIsStamped proves service/env/version reach every line, so
// a log search can scope to one deployment without the call site helping.
func TestProcessIdentityIsStamped(t *testing.T) {
	t.Parallel()
	sink := logging.NewSink()
	l, _ := logging.New(logging.Config{
		Service: "api", Env: "production", Version: "1.2.3", Commit: "abc1234",
		InstanceID: "i-9", Level: slog.LevelInfo, Format: logging.FormatJSON, Out: sink,
	})
	l.InfoContext(context.Background(), "boot")
	last := sink.Last()
	for k, want := range map[string]any{
		logkey.Service: "api", logkey.Env: "production",
		logkey.Version: "1.2.3", logkey.Commit: "abc1234", logkey.Instance: "i-9",
	} {
		if last[k] != want {
			t.Errorf("%s = %v, want %v", k, last[k], want)
		}
	}
}

// TestLevelKnobIsLive proves an incident can raise verbosity without a restart,
// which is the whole reason New returns the LevelVar.
func TestLevelKnobIsLive(t *testing.T) {
	t.Parallel()
	sink := logging.NewSink()
	l, level := logging.New(logging.Config{Level: slog.LevelInfo, Format: logging.FormatJSON, Out: sink})

	l.DebugContext(context.Background(), "quiet")
	if len(sink.Lines()) != 0 {
		t.Fatalf("debug emitted while level is info:\n%s", sink.String())
	}
	level.Set(slog.LevelDebug)
	l.DebugContext(context.Background(), "loud")
	if len(sink.Lines()) != 1 {
		t.Fatalf("debug not emitted after raising the level:\n%s", sink.String())
	}
}
