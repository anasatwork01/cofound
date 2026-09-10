package logging

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/anasatwork01/cofound/packages/chassis/logkey"
	"github.com/anasatwork01/cofound/packages/chassis/scope"
)

// Attrs returns the correlation fields carried by ctx.
//
// Reading them from the context rather than threading a *slog.Logger is what
// slog's own contract intends: Handler.Handle's godoc says its ctx "is present
// solely to provide Handlers access to the context's values".
func Attrs(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}
	out := make([]slog.Attr, 0, 8)

	if id := scope.RequestID(ctx); id != "" {
		out = append(out, slog.String(logkey.RequestID, id))
	}

	s := scope.From(ctx)
	if s.OrgID != "" {
		out = append(out, slog.String(logkey.OrgID, s.OrgID))
	}
	if s.ProjectID != "" {
		out = append(out, slog.String(logkey.ProjectID, s.ProjectID))
	}
	if s.SessionID != "" {
		out = append(out, slog.String(logkey.SessionID, s.SessionID))
	}
	if s.Turn != 0 {
		out = append(out, slog.Int(logkey.Turn, s.Turn))
	}

	// trace_id and span_id are separate fields rather than a synthesised
	// correlation id: the join happens in the log, where it costs nothing.
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		out = append(out,
			slog.String(logkey.TraceID, sc.TraceID().String()),
			slog.String(logkey.SpanID, sc.SpanID().String()),
			slog.Bool(logkey.Sampled, sc.IsSampled()),
		)
	}
	return out
}
