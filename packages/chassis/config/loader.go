package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/logkey"
)

// Lookup reads one environment variable. It is injected rather than calling
// os.LookupEnv directly because t.Setenv still panics alongside t.Parallel in
// Go 1.27, so a package-level environment would serialise every config test.
type Lookup func(key string) (string, bool)

// OSLookup reads the process environment.
func OSLookup() Lookup { return os.LookupEnv }

// MapLookup reads a fixed map, for tests.
func MapLookup(m map[string]string) Lookup {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

// Loader accumulates problems instead of returning on the first one.
//
// Every accessor records a problem and returns the default, so one pass fills a
// whole config struct and Err reports everything at once. A service binds its
// own fields on the same Loader, so a deploy missing DATABASE_URL and REDIS_URL
// is told about both.
type Loader struct {
	get      Lookup
	problems []Problem
	resolved []resolvedKey
	secrets  map[string]struct{}
	provided map[string]bool
}

type resolvedKey struct {
	key    string
	value  slog.Value
	secret bool
}

// NewLoader returns a Loader reading through get.
func NewLoader(get Lookup) *Loader {
	if get == nil {
		get = OSLookup()
	}
	return &Loader{
		get:      get,
		secrets:  map[string]struct{}{},
		provided: map[string]bool{},
	}
}

// Err returns an *Error if anything went wrong, else nil.
func (l *Loader) Err() error {
	if len(l.problems) == 0 {
		return nil
	}
	return &Error{Problems: slices.Clone(l.problems)}
}

// Problemf records a problem. The format arguments must describe the shape
// wanted, never the value received.
func (l *Loader) Problemf(key, expect string, a ...any) {
	if len(a) > 0 {
		expect = fmt.Sprintf(expect, a...)
	}
	l.problems = append(l.problems, Problem{Key: key, Expect: expect})
}

// MarkSecret records that a key's value must never be logged in full. Call it
// before or after reading; Resolved consults the set at render time.
func (l *Loader) MarkSecret(keys ...string) {
	for _, k := range keys {
		l.secrets[k] = struct{}{}
	}
}

// Set reports whether the key was explicitly provided, as opposed to defaulted.
func (l *Loader) Set(key string) bool { return l.provided[key] }

// raw reads a key. A present-but-blank variable is treated as absent: on a
// container platform an empty value is the same operational mistake as an unset
// one, and distinguishing them only produces confusing errors.
func (l *Loader) raw(key string) (string, bool) {
	v, ok := l.get(key)
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	l.provided[key] = true
	return v, true
}

func (l *Loader) record(key string, v slog.Value) {
	_, secret := l.secrets[key]
	l.resolved = append(l.resolved, resolvedKey{key: key, value: v, secret: secret})
}

// String reads a string, defaulting to def.
func (l *Loader) String(key, def string) string {
	v, ok := l.raw(key)
	if !ok {
		v = def
	}
	l.record(key, slog.StringValue(v))
	return v
}

// RequiredString reads a string and records a problem when it is absent.
func (l *Loader) RequiredString(key string) string {
	v, ok := l.raw(key)
	if !ok {
		l.Problemf(key, "is required")
	}
	l.record(key, slog.StringValue(v))
	return v
}

// Enum reads one of allowed, defaulting to def.
func (l *Loader) Enum(key, def string, allowed ...string) string {
	v, ok := l.raw(key)
	if !ok {
		v = def
	}
	if !slices.Contains(allowed, v) {
		l.Problemf(key, "expect one of %s", strings.Join(allowed, ", "))
		v = def
	}
	l.record(key, slog.StringValue(v))
	return v
}

// Int reads an integer within [min, max].
func (l *Loader) Int(key string, def, minVal, maxVal int) int {
	v := def
	if s, ok := l.raw(key); ok {
		n, err := strconv.Atoi(s)
		switch {
		case err != nil:
			l.Problemf(key, "expect an integer")
		case n < minVal || n > maxVal:
			l.Problemf(key, "expect an integer in [%d, %d]", minVal, maxVal)
		default:
			v = n
		}
	}
	l.record(key, slog.IntValue(v))
	return v
}

// Bytes reads a byte count, accepting a plain integer or a KB/MB/GB suffix.
func (l *Loader) Bytes(key string, def int) int {
	v := def
	if s, ok := l.raw(key); ok {
		if n, err := parseBytes(s); err != nil {
			l.Problemf(key, "expect a byte count, e.g. 1048576, 1MB or 512KB")
		} else if n <= 0 {
			l.Problemf(key, "expect a positive byte count")
		} else {
			v = n
		}
	}
	l.record(key, slog.IntValue(v))
	return v
}

func parseBytes(s string) (int, error) {
	up := strings.ToUpper(strings.TrimSpace(s))
	mult := 1
	switch {
	case strings.HasSuffix(up, "GB"):
		mult, up = 1<<30, strings.TrimSuffix(up, "GB")
	case strings.HasSuffix(up, "MB"):
		mult, up = 1<<20, strings.TrimSuffix(up, "MB")
	case strings.HasSuffix(up, "KB"):
		mult, up = 1<<10, strings.TrimSuffix(up, "KB")
	case strings.HasSuffix(up, "B"):
		up = strings.TrimSuffix(up, "B")
	}
	n, err := strconv.Atoi(strings.TrimSpace(up))
	if err != nil {
		return 0, err
	}
	return n * mult, nil
}

// Port reads a TCP port.
func (l *Loader) Port(key string, def int) int {
	return l.Int(key, def, 1, 65535)
}

// Bool reads a boolean.
func (l *Loader) Bool(key string, def bool) bool {
	v := def
	if s, ok := l.raw(key); ok {
		b, err := strconv.ParseBool(s)
		if err != nil {
			l.Problemf(key, "expect a boolean, e.g. true or false")
		} else {
			v = b
		}
	}
	l.record(key, slog.BoolValue(v))
	return v
}

// Duration reads a Go duration within [min, max].
func (l *Loader) Duration(key string, def, minVal, maxVal time.Duration) time.Duration {
	v := def
	if s, ok := l.raw(key); ok {
		d, err := time.ParseDuration(s)
		switch {
		case err != nil:
			l.Problemf(key, "expect a duration, e.g. 30s, 500ms or 2m")
		case d < minVal || d > maxVal:
			l.Problemf(key, "expect a duration in [%s, %s]", minVal, maxVal)
		default:
			v = d
		}
	}
	l.record(key, slog.StringValue(v.String()))
	return v
}

// Float reads a float within [min, max].
func (l *Loader) Float(key string, def, minVal, maxVal float64) float64 {
	v := def
	if s, ok := l.raw(key); ok {
		f, err := strconv.ParseFloat(s, 64)
		switch {
		case err != nil:
			l.Problemf(key, "expect a number")
		case f < minVal || f > maxVal:
			l.Problemf(key, "expect a number in [%g, %g]", minVal, maxVal)
		default:
			v = f
		}
	}
	l.record(key, slog.Float64Value(v))
	return v
}

// OptionalFloat reads a float, reporting whether it was provided. Used where
// leaving a knob untouched is different from setting it to a default — the OTel
// sampler is the case that matters.
func (l *Loader) OptionalFloat(key string, minVal, maxVal float64) (float64, bool) {
	s, ok := l.raw(key)
	if !ok {
		l.record(key, slog.StringValue("unset"))
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	switch {
	case err != nil:
		l.Problemf(key, "expect a number")
		l.record(key, slog.StringValue("invalid"))
		return 0, false
	case f < minVal || f > maxVal:
		l.Problemf(key, "expect a number in [%g, %g]", minVal, maxVal)
		l.record(key, slog.StringValue("invalid"))
		return 0, false
	}
	l.record(key, slog.Float64Value(f))
	return f, true
}

// Level reads a slog level.
func (l *Loader) Level(key string, def slog.Level) slog.Level {
	v := def
	if s, ok := l.raw(key); ok {
		var lv slog.Level
		if err := lv.UnmarshalText([]byte(s)); err != nil {
			l.Problemf(key, "expect a log level: debug, info, warn or error")
		} else {
			v = lv
		}
	}
	l.record(key, slog.StringValue(strings.ToLower(v.String())))
	return v
}

// URL reads a URL, optionally constrained to schemes.
func (l *Loader) URL(key, def string, schemes ...string) *url.URL {
	s, ok := l.raw(key)
	if !ok {
		s = def
	}
	if s == "" {
		l.record(key, slog.StringValue(""))
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		l.Problemf(key, "expect an absolute URL")
		l.record(key, slog.StringValue("invalid"))
		return nil
	}
	if len(schemes) > 0 && !slices.Contains(schemes, u.Scheme) {
		l.Problemf(key, "expect a URL with scheme %s", strings.Join(schemes, " or "))
		l.record(key, slog.StringValue("invalid"))
		return nil
	}
	l.record(key, slog.StringValue(u.Redacted()))
	return u
}

// SecretURL reads a URL whose userinfo is a credential. The key is marked
// secret, so Resolved renders a fingerprint rather than the value.
func (l *Loader) SecretURL(key string, required bool, schemes ...string) *url.URL {
	l.MarkSecret(key)
	s, ok := l.raw(key)
	if !ok {
		if required {
			l.Problemf(key, "is required")
		}
		l.record(key, slog.StringValue(""))
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		l.Problemf(key, "expect an absolute URL")
		l.record(key, slog.StringValue("invalid"))
		return nil
	}
	if len(schemes) > 0 && !slices.Contains(schemes, u.Scheme) {
		l.Problemf(key, "expect a URL with scheme %s", strings.Join(schemes, " or "))
		l.record(key, slog.StringValue("invalid"))
		return nil
	}
	l.record(key, slog.StringValue(s))
	return u
}

// CIDRs reads a comma-separated list of CIDR prefixes.
func (l *Loader) CIDRs(key string) []netip.Prefix {
	s, ok := l.raw(key)
	if !ok {
		l.record(key, slog.IntValue(0))
		return nil
	}
	var out []netip.Prefix
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			l.Problemf(key, "expect a comma-separated list of CIDR prefixes, e.g. 10.0.0.0/8")
			l.record(key, slog.StringValue("invalid"))
			return nil
		}
		out = append(out, p)
	}
	l.record(key, slog.IntValue(len(out)))
	return out
}

// CSV reads a comma-separated list of strings.
func (l *Loader) CSV(key string, def ...string) []string {
	s, ok := l.raw(key)
	if !ok {
		l.record(key, slog.AnyValue(def))
		return slices.Clone(def)
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	l.record(key, slog.AnyValue(out))
	return out
}

// KeyValues reads a comma-separated key=value list, as
// OTEL_EXPORTER_OTLP_HEADERS uses. Values are never echoed: the header list is
// where an OTLP bearer token lives.
func (l *Loader) KeyValues(key string) map[string]string {
	l.MarkSecret(key)
	s, ok := l.raw(key)
	if !ok {
		l.record(key, slog.IntValue(0))
		return nil
	}
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, found := strings.Cut(part, "=")
		if !found || strings.TrimSpace(k) == "" {
			l.Problemf(key, "expect a comma-separated key=value list")
			l.record(key, slog.StringValue("invalid"))
			return nil
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	l.record(key, slog.IntValue(len(out)))
	return out
}

// Resolved returns one attribute per key read, with its effective value.
//
// A secret key renders as "unset" or "set (sha256:xxxxxxxx)" — enough to tell
// whether two replicas booted with the same DATABASE_URL at 3am, never enough
// to authenticate with.
func (l *Loader) Resolved() []slog.Attr {
	out := make([]slog.Attr, 0, len(l.resolved))
	for _, r := range l.resolved {
		v := r.value
		if r.secret {
			v = slog.StringValue(fingerprint(r.value))
		}
		out = append(out, slog.Attr{Key: r.key, Value: v})
	}
	return out
}

func fingerprint(v slog.Value) string {
	s := v.String()
	if s == "" || s == "0" {
		return "unset"
	}
	sum := sha256.Sum256([]byte(s))
	return "set (sha256:" + hex.EncodeToString(sum[:4]) + ")"
}

// ResolvedGroup returns Resolved as a single grouped attribute, for the one
// boot line that records the effective configuration.
func (l *Loader) ResolvedGroup() slog.Attr {
	attrs := l.Resolved()
	anys := make([]any, 0, len(attrs))
	for _, a := range attrs {
		anys = append(anys, a)
	}
	return slog.Group(logkey.ConfigKey, anys...)
}
