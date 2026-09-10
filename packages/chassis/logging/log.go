package logging

import (
	"context"
	"log/slog"
)

// This file exposes context-first functions only, and no Info(msg) overload.
//
// slog.Logger.Info passes context.Background(), so a line logged through it
// carries no request_id and no trace_id — a one-word difference from
// InfoContext, with no compile error and no vet warning. sloglint's ContextOnly
// rule is the tool that catches it, but adopting a linter changes this repo's
// CI shape (see the deferred list in the task plan). Removing the overload
// closes the hole for chassis-shaped call sites at no cost.

type loggerKey struct{}

// WithLogger stores l in ctx.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	if l == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerKey{}, l)
}

// L returns the logger in ctx, or the process default. Never nil.
func L(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && l != nil {
			return l
		}
	}
	return slog.Default()
}

// Debug logs at debug level. User content is revealed at this level only.
func Debug(ctx context.Context, msg string, args ...any) {
	L(ctx).DebugContext(ctx, msg, args...)
}

// Info logs at info level.
func Info(ctx context.Context, msg string, args ...any) {
	L(ctx).InfoContext(ctx, msg, args...)
}

// Warn logs at warn level.
func Warn(ctx context.Context, msg string, args ...any) {
	L(ctx).WarnContext(ctx, msg, args...)
}

// Error logs at error level.
func Error(ctx context.Context, msg string, args ...any) {
	L(ctx).ErrorContext(ctx, msg, args...)
}
