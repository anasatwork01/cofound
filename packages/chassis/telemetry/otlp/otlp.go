// Package otlp is the OTLP span exporter, deliberately isolated in its own leaf
// package.
//
// IMPORTING THIS PACKAGE adds roughly 65 modules and 10MB to a binary
// (measured: 12.3MB to 22.3MB). Because Go links per package, a service that
// never imports it links none of that. Import it from a service main, never
// from chassis code.
//
// The gRPC weight is unavoidable either way: otlptracehttp's own
// internal/otlpconfig imports google.golang.org/grpc, so choosing
// otlptracegrpc would save nothing. HTTP is chosen for the better proxy and
// ingress story.
package otlp

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/anasatwork01/cofound/packages/chassis/telemetry"
)

// Factory satisfies telemetry.ExporterFactory.
//
// Configuration comes from the standard OTEL_EXPORTER_OTLP_* variables, which
// the exporter reads itself; the Config values are applied on top so an
// explicitly-bound endpoint wins.
//
// The connection is LAZY. An unreachable collector does not fail construction
// (~120us measured) and surfaces only through the error handler, so a dead
// collector never stops a service booting.
func Factory(ctx context.Context, c telemetry.Config) (sdktrace.SpanExporter, error) {
	opts := []otlptracehttp.Option{}
	if c.Endpoint != "" {
		opts = append(opts, otlptracehttp.WithEndpointURL(c.Endpoint))
	}
	if len(c.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(c.Headers))
	}
	return otlptracehttp.New(ctx, opts...)
}
