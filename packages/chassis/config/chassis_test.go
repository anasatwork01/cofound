package config_test

import (
	"strings"
	"testing"

	"github.com/anasatwork01/cofound/packages/chassis/config"
)

const testDSN = "https://0123456789abcdef0123456789abcdef@o0.ingest.example.invalid/42"

func resolve(t *testing.T, env map[string]string) (*config.Chassis, *config.Loader) {
	t.Helper()
	l := config.NewLoader(config.MapLookup(env))
	c := &config.Chassis{}
	c.Bind(l, config.Defaults{Service: "api", Version: "1.4.2", Commit: "abc1234"})
	return c, l
}

func resolvedValue(t *testing.T, l *config.Loader, key string) string {
	t.Helper()
	for _, a := range l.Resolved() {
		if a.Key == key {
			return a.Value.String()
		}
	}
	t.Fatalf("%s is missing from the resolved configuration", key)
	return ""
}

// TestSentryDSNIsFingerprintedNotEchoed.
//
// The DSN is bound through the loader rather than left for sentry-go to read
// out of the environment, which means it lands in the one boot line that
// records effective configuration. That line has to stay comparable between two
// replicas at 3am and never sufficient to authenticate with.
//
// Asserted on Resolved() rather than on log output on purpose: the logging
// Guard also redacts any key containing "dsn", so a log-level assertion would
// pass even if this binding stopped marking the key secret.
func TestSentryDSNIsFingerprintedNotEchoed(t *testing.T) {
	t.Parallel()
	c, l := resolve(t, map[string]string{config.KeySentryDSN: testDSN})
	if err := l.Err(); err != nil {
		t.Fatalf("a valid DSN must not be a config problem: %v", err)
	}
	if c.Sentry.DSN != testDSN {
		t.Errorf("Sentry.DSN = %q, want the DSN verbatim — sentry-go needs the real value", c.Sentry.DSN)
	}

	got := resolvedValue(t, l, config.KeySentryDSN)
	if strings.Contains(got, "0123456789abcdef") {
		t.Errorf("the boot line would echo the ingest key: %s", got)
	}
	if !strings.HasPrefix(got, "set (sha256:") {
		t.Errorf("%s resolved as %q, want a fingerprint", config.KeySentryDSN, got)
	}
}

// TestNoSentryDSNResolvesToEmpty. Empty is the ONLY switch the reporter has,
// and observability/sentry.Init refuses to build a client without one.
func TestNoSentryDSNResolvesToEmpty(t *testing.T) {
	t.Parallel()
	c, l := resolve(t, map[string]string{})
	if err := l.Err(); err != nil {
		t.Fatalf("an absent DSN must not be a config problem: %v", err)
	}
	if c.Sentry.DSN != "" {
		t.Errorf("Sentry.DSN = %q, want empty", c.Sentry.DSN)
	}
	if got := resolvedValue(t, l, config.KeySentryDSN); got != "unset" {
		t.Errorf("%s resolved as %q, want unset", config.KeySentryDSN, got)
	}
}

// TestAMalformedSentryDSNIsAConfigProblem, reported in the same pass as every
// other bad variable rather than as a separate crash later in boot. A service
// that starts with a typo'd DSN reports nothing, forever, silently.
func TestAMalformedSentryDSNIsAConfigProblem(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{
		"not-a-url",
		"ftp://key@example.invalid/1",
	} {
		t.Run(bad, func(t *testing.T) {
			t.Parallel()
			c, l := resolve(t, map[string]string{config.KeySentryDSN: bad})
			err := l.Err()
			if err == nil {
				t.Fatalf("%q was accepted as a DSN", bad)
			}
			if !strings.Contains(err.Error(), config.KeySentryDSN) {
				t.Errorf("the problem does not name the key:\n%s", err)
			}
			if strings.Contains(err.Error(), bad) {
				t.Errorf("a config problem must describe the shape wanted, never echo the value:\n%s", err)
			}
			if c.Sentry.DSN != "" {
				t.Errorf("a rejected DSN must not reach the reporter: %q", c.Sentry.DSN)
			}
		})
	}
}
