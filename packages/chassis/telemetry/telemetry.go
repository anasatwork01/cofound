package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/anasatwork01/cofound/packages/chassis/clock"
)

// SchemaURL is the telemetry schema this service reports.
//
// semconv v1.43.0 is the highest schema bundled inside otel v1.46.0; semconv is
// not a separate module. Bump the import path as its own commit — the schema
// URL is exported telemetry contract that a backend keys off.
const SchemaURL = semconv.SchemaURL

// Config describes the tracing setup.
type Config struct {
	Service    string
	Version    string
	Commit     string
	Env        string
	InstanceID string

	Endpoint string
	Protocol string
	Headers  map[string]string

	SampleRatio    float64
	SampleRatioSet bool

	ShutdownTimeout time.Duration
	Extra           []attribute.KeyValue
}

// ExporterFactory builds a span exporter. It is a seam so the chassis core
// links no gRPC: importing the OTLP exporter costs roughly 65 modules and 10MB
// of binary, and Go links per package, so a service that never imports it pays
// nothing.
type ExporterFactory func(context.Context, Config) (sdktrace.SpanExporter, error)

// Options configures Setup.
type Options struct {
	Config Config

	// Exporter is a pre-built exporter, for tests (tracetest.NewSpanRecorder).
	Exporter sdktrace.SpanExporter
	// Factory is used when Exporter is nil and Endpoint is set.
	Factory ExporterFactory

	Log   *slog.Logger
	Clock clock.Clock

	// Sync uses WithSyncer instead of WithBatcher, so a test can assert on
	// spans without flushing.
	Sync bool
	// SetGlobals installs the provider and propagator in otel's global
	// registry. Tests leave it false so they can run in parallel.
	SetGlobals bool
}

// Provider owns the tracer provider and its shutdown.
type Provider struct {
	tp       trace.TracerProvider
	shutdown func(context.Context) error
	enabled  bool
}

// Setup builds the provider.
//
// It never blocks on a collector: the OTLP exporter's connection is lazy, so an
// unreachable collector surfaces through the error handler rather than delaying
// boot or failing it.
func Setup(ctx context.Context, o Options) (*Provider, error) {
	clk := o.Clock
	if clk == nil {
		clk = clock.Real()
	}
	if o.Log != nil {
		// The SDK's default error handler writes straight to stderr outside
		// the JSON format, which breaks structured ingestion the first time a
		// collector hiccups — and an unreachable collector produces a
		// continuous stream of "connection refused", which is exactly the
		// noise that trains people to ignore error output.
		otel.SetErrorHandler(NewErrorHandler(o.Log, clk, time.Minute))
	}

	// The propagator is installed even when tracing is off, so a service with
	// no exporter still forwards an inbound traceparent.
	if o.SetGlobals {
		otel.SetTextMapPropagator(Propagator())
	}

	exp := o.Exporter
	if exp == nil && o.Config.Endpoint != "" {
		if o.Factory == nil {
			return nil, errors.New("telemetry: an OTLP endpoint is configured but no exporter factory was provided; pass otlp.Factory from the service main")
		}
		built, err := o.Factory(ctx, o.Config)
		if err != nil {
			return nil, err
		}
		exp = built
	}

	if exp == nil {
		p := &Provider{tp: noop.NewTracerProvider(), shutdown: func(context.Context) error { return nil }}
		if o.SetGlobals {
			otel.SetTracerProvider(p.tp)
		}
		return p, nil
	}

	res, err := Resource(o.Config)
	if err != nil {
		return nil, err
	}

	opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if o.Sync {
		opts = append(opts, sdktrace.WithSyncer(exp))
	} else {
		// Never WithBlocking: a slow collector must not add latency to a user
		// turn.
		opts = append(opts, sdktrace.WithBatcher(exp,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxQueueSize(2048),
			sdktrace.WithMaxExportBatchSize(512),
			sdktrace.WithExportTimeout(30*time.Second),
		))
	}
	if o.Config.SampleRatioSet {
		// Only when explicitly set: passing WithSampler overrides
		// OTEL_TRACES_SAMPLER and OTEL_TRACES_SAMPLER_ARG, taking sampling
		// control away from operators.
		//
		// ParentBased is load-bearing even at ratio 0.0 — it is what lets a
		// sampled console trace continue through api and sandboxd.
		opts = append(opts, sdktrace.WithSampler(
			sdktrace.ParentBased(sdktrace.TraceIDRatioBased(o.Config.SampleRatio)),
		))
	}

	tp := sdktrace.NewTracerProvider(opts...)
	if o.SetGlobals {
		otel.SetTracerProvider(tp)
	}
	return &Provider{tp: tp, shutdown: tp.Shutdown, enabled: true}, nil
}

// TracerProvider returns the provider, no-op when tracing is disabled.
func (p *Provider) TracerProvider() trace.TracerProvider { return p.tp }

// Tracer returns a named tracer.
func (p *Provider) Tracer(name string) trace.Tracer { return p.tp.Tracer(name) }

// Enabled reports whether spans are actually exported.
func (p *Provider) Enabled() bool { return p.enabled }

// Shutdown flushes pending spans.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || p.shutdown == nil {
		return nil
	}
	return p.shutdown(ctx)
}

// Propagator is W3C tracecontext plus baggage.
//
// The baggage half is what carries the tenancy tuple across a process boundary,
// so a span in sandboxd knows which org it belongs to.
func Propagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

// Resource describes this process.
func Resource(c Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{}
	if c.Service != "" {
		attrs = append(attrs, semconv.ServiceName(c.Service))
	}
	if c.Version != "" {
		attrs = append(attrs, semconv.ServiceVersion(c.Version))
	}
	if c.InstanceID != "" {
		attrs = append(attrs, semconv.ServiceInstanceID(c.InstanceID))
	}
	if c.Env != "" {
		// The generated helpers are inconsistent: ServiceName(v) exists,
		// DeploymentEnvironmentName(v) does not. Only the Key constant.
		attrs = append(attrs, semconv.DeploymentEnvironmentNameKey.String(c.Env))
	}
	if c.Commit != "" {
		attrs = append(attrs, attribute.String("halyard.build.commit", c.Commit))
	}
	attrs = append(attrs, c.Extra...)

	return resource.New(context.Background(),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithProcessRuntimeVersion(),
		resource.WithSchemaURL(SchemaURL),
		resource.WithAttributes(attrs...),
	)
}
