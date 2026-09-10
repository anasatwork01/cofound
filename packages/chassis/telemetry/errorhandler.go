package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/clock"
	"github.com/anasatwork01/cofound/packages/chassis/logkey"
)

// ErrorHandler receives SDK-internal errors and rate-limits them into the
// structured log.
//
// Two problems it solves. The SDK's default handler writes to stderr outside
// the JSON format, so the first collector hiccup breaks structured ingestion.
// And an unreachable collector produces a continuous stream of identical
// "connection refused" lines — the kind of persistent noise that teaches people
// to ignore error output entirely, which is worse than silence.
type ErrorHandler struct {
	log   *slog.Logger
	clk   clock.Clock
	every time.Duration

	mu         sync.Mutex
	lastLogged time.Time
	suppressed int
}

// NewErrorHandler rate-limits to one line per every.
func NewErrorHandler(log *slog.Logger, clk clock.Clock, every time.Duration) *ErrorHandler {
	if clk == nil {
		clk = clock.Real()
	}
	if every <= 0 {
		every = time.Minute
	}
	return &ErrorHandler{log: log, clk: clk, every: every}
}

// Handle implements otel.ErrorHandler.
func (h *ErrorHandler) Handle(err error) {
	if err == nil || h.log == nil {
		return
	}
	now := h.clk.Now()

	h.mu.Lock()
	if !h.lastLogged.IsZero() && now.Sub(h.lastLogged) < h.every {
		h.suppressed++
		h.mu.Unlock()
		return
	}
	suppressed := h.suppressed
	h.suppressed = 0
	h.lastLogged = now
	h.mu.Unlock()

	attrs := []slog.Attr{
		slog.String(logkey.Component, "otel"),
		slog.String(logkey.Err, err.Error()),
	}
	if suppressed > 0 {
		// Reporting the count is what keeps rate limiting honest: a reader can
		// tell "one blip" from "continuously broken".
		attrs = append(attrs, slog.Int(logkey.Count, suppressed))
	}
	// Warn, not Error: telemetry export failing degrades observability, it does
	// not fail a user request, and paging on it trains people to ignore pages.
	h.log.LogAttrs(context.Background(), slog.LevelWarn, "telemetry export problem", attrs...)
}

// Suppressed reports how many errors are currently folded into the next line.
func (h *ErrorHandler) Suppressed() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.suppressed
}
