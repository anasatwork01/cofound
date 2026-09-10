package httpx

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// RouteTag records the matched route pattern on the span and the metrics
// labeler.
//
// It runs post-routing by construction — it calls next first — because that is
// the only point at which chi.RouteContext().RoutePattern() is populated.
//
// Required with ANY router. otelhttp v0.71.0 renames the server span after the
// handler when r.Pattern is set, which chi v5.3.2 does, so span NAMES are
// already low-cardinality. But otelhttp never calls HTTPServer.Route and passes
// no route into metric attributes, so http.route has to come from here.
// WithRouteTag was removed in v0.71.0.
func RouteTag(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)

		pattern := ""
		if rc := chi.RouteContext(r.Context()); rc != nil {
			pattern = rc.RoutePattern()
		}
		if pattern == "" {
			// A 404 or 405 has no pattern. Tagging the raw path here would
			// reintroduce unbounded cardinality through the back door.
			return
		}
		attr := semconv.HTTPRoute(pattern)
		trace.SpanFromContext(r.Context()).SetAttributes(attr)
		if labeler, ok := otelhttp.LabelerFromContext(r.Context()); ok {
			labeler.Add(attr)
		}
	})
}
