package logging

import (
	"context"
	"log/slog"
)

// Guard is the handler that enforces SPEC 17.3.
//
// It wraps another handler and, on every path — direct attributes, With,
// WithGroup, and nested groups — redacts secret-keyed values, suppresses
// messages containing credential shapes, and hides user content unless the
// effective level is Debug. It also stamps the context's correlation fields, so
// no call site has to remember them.
//
// A group whose own key is secret-shaped redacts its entire subtree. That case
// is the specific reason this is a handler and not a ReplaceAttr hook:
// ReplaceAttr is never shown a group's key, so slog.Group("token", "a", secret)
// passes through it untouched.
type Guard struct {
	next  slog.Handler
	level *slog.LevelVar

	// redactAll is set once any enclosing group key looked secret-shaped.
	redactAll bool
}

// NewGuard wraps next. level may be nil, in which case next's own Enabled
// decides.
func NewGuard(next slog.Handler, level *slog.LevelVar) *Guard {
	return &Guard{next: next, level: level}
}

// Enabled implements slog.Handler.
func (g *Guard) Enabled(ctx context.Context, l slog.Level) bool {
	if g.level != nil {
		return l >= g.level.Level()
	}
	return g.next.Enabled(ctx, l)
}

// Handle implements slog.Handler.
func (g *Guard) Handle(ctx context.Context, r slog.Record) error {
	debug := r.Level <= slog.LevelDebug

	msg := r.Message
	if pattern, hit := ScanMessage(msg); hit {
		// Keep the fact, drop the payload. Naming the pattern rather than the
		// value tells an operator which integration to fix.
		msg = SuppressedMessage
		out := slog.NewRecord(r.Time, r.Level, msg, r.PC)
		out.AddAttrs(slog.String("suppressed_pattern", pattern))
		out.AddAttrs(Attrs(ctx)...)
		return g.next.Handle(ctx, out)
	}

	out := slog.NewRecord(r.Time, r.Level, msg, r.PC)
	out.AddAttrs(Attrs(ctx)...)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(g.clean(a, debug))
		return true
	})
	return g.next.Handle(ctx, out)
}

// WithAttrs implements slog.Handler. Attributes are cleaned as they are added,
// so a logger built once with a secret attribute cannot leak it on every
// subsequent line.
func (g *Guard) WithAttrs(as []slog.Attr) slog.Handler {
	if len(as) == 0 {
		return g
	}
	cleaned := make([]slog.Attr, 0, len(as))
	for _, a := range as {
		// With-attributes are pre-resolved, before any record's level is
		// known, so user content must stay wrapped here: the marker renders
		// itself and Handle can still unwrap per record at Debug.
		cleaned = append(cleaned, g.clean(a, false))
	}
	return &Guard{next: g.next.WithAttrs(cleaned), level: g.level, redactAll: g.redactAll}
}

// WithGroup implements slog.Handler.
func (g *Guard) WithGroup(name string) slog.Handler {
	if name == "" {
		return g
	}
	return &Guard{
		next:      g.next.WithGroup(name),
		level:     g.level,
		redactAll: g.redactAll || IsSecretKey(name),
	}
}

// clean redacts a single attribute, recursing into groups.
func (g *Guard) clean(a slog.Attr, debug bool) slog.Attr {
	if g.redactAll {
		return redactAttr(a)
	}
	if IsSecretKey(a.Key) {
		return redactAttr(a)
	}

	// A group's key was not secret-shaped, but a member's may be. Recurse
	// before resolving, so a nested slog.Group("token", ...) is still caught.
	if a.Value.Kind() == slog.KindGroup {
		members := a.Value.Group()
		cleaned := make([]slog.Attr, 0, len(members))
		for _, m := range members {
			cleaned = append(cleaned, g.clean(m, debug))
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(cleaned...)}
	}

	if IsUserContent(a) {
		if !debug {
			// Leaving it wrapped would also work — LogValue renders the marker
			// — but replacing it here keeps the emitted shape identical
			// whether or not the sink resolves LogValuers.
			return slog.String(a.Key, OmittedUserContent)
		}
		if u, ok := a.Value.Any().(userValue); ok {
			return slog.Any(a.Key, u.unwrap())
		}
	}
	return a
}

func redactAttr(a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindGroup {
		// Redact the whole subtree rather than dropping it: the shape of what
		// was logged stays visible, the values do not.
		members := a.Value.Group()
		cleaned := make([]slog.Attr, 0, len(members))
		for _, m := range members {
			cleaned = append(cleaned, redactAttr(m))
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(cleaned...)}
	}
	return slog.String(a.Key, Redacted)
}

var _ slog.Handler = (*Guard)(nil)
