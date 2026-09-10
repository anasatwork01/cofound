package logging

import "log/slog"

// OmittedUserContent replaces user content above debug level.
const OmittedUserContent = "[user content omitted]"

// userValue wraps a value that may contain user content.
//
// It implements slog.LogValuer returning the marker, so it fails CLOSED through
// any handler — including a plain slog.NewJSONHandler that has never heard of
// this package. Only Guard unwraps it, and only at LevelDebug. That ordering
// matters: a marker that fails open would leak the first time someone logged
// through a handler the chassis did not build.
type userValue struct{ v any }

// LogValue implements slog.LogValuer.
func (userValue) LogValue() slog.Value { return slog.StringValue(OmittedUserContent) }

func (u userValue) unwrap() any { return u.v }

// User marks a value as user content.
func User(key string, v any) slog.Attr {
	return slog.Any(key, userValue{v: v})
}

// Prompt marks an agent prompt as user content.
func Prompt(v string) slog.Attr { return User("prompt", v) }

// Output marks model output as user content.
func Output(v string) slog.Attr { return User("output", v) }

// Diff marks a code diff as user content.
func Diff(v string) slog.Attr { return User("diff", v) }

// IsUserContent reports whether a is marked user content.
//
// Both kinds must be checked. slog.AnyValue on a type implementing LogValuer
// returns a value of kind KindLogValuer, not KindAny — so a check for KindAny
// alone silently never matches, and the marker would then be redacted at every
// level including Debug, making support impossible.
func IsUserContent(a slog.Attr) bool {
	switch a.Value.Kind() {
	case slog.KindAny, slog.KindLogValuer:
		_, ok := a.Value.Any().(userValue)
		return ok
	default:
		return false
	}
}
