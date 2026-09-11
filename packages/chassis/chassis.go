package chassis

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/anasatwork01/cofound/packages/chassis/clock"
	"github.com/anasatwork01/cofound/packages/chassis/config"
	"github.com/anasatwork01/cofound/packages/chassis/health"
	"github.com/anasatwork01/cofound/packages/chassis/httpx"
	"github.com/anasatwork01/cofound/packages/chassis/lifecycle"
	"github.com/anasatwork01/cofound/packages/chassis/logging"
	"github.com/anasatwork01/cofound/packages/chassis/logkey"
	"github.com/anasatwork01/cofound/packages/chassis/observability"
	"github.com/anasatwork01/cofound/packages/chassis/telemetry"
)

// ExitConfig is returned when configuration is invalid.
//
// 78 is EX_CONFIG. A crash-looping container's last line then names every
// variable to fix, and the exit code distinguishes "you configured this wrong"
// from "it crashed".
const ExitConfig = 78

// Service is what a service main describes.
type Service struct {
	// Name, Version and Commit come from package main, because the Makefile
	// injects -X main.version and -X main.commit. A chassis cannot own those
	// symbols without changing the ldflags paths.
	Name    string
	Version string
	Commit  string

	// Bind binds the service's own configuration on the SHARED Loader, so one
	// boot reports the union of chassis and service problems.
	Bind func(l *config.Loader)

	// Setup builds dependencies and mounts routes. It runs after config,
	// logging, tracing and the router exist, and before the listener opens.
	// The returned Closer runs after the drain, so a pool closes only once no
	// handler can reach it.
	Setup func(ctx context.Context, rt *Runtime) (io.Closer, error)

	// Probes registers readiness probes. Runs after Setup.
	Probes func(rt *Runtime, reg *health.Registry)

	// Exporter opts this binary into an OTLP exporter. nil links none —
	// importing telemetry/otlp costs roughly 65 modules and 10MB.
	Exporter telemetry.ExporterFactory

	// Reporter opts this binary into error reporting (SPEC 17.3). nil links
	// none, and so does a bound factory with no SENTRY_DSN — see
	// observability/sentry.Init, where "no DSN" has to mean "no client" rather
	// than "a disabled client", because a disabled sentry-go client still runs
	// a 100ms ticker for the life of the process.
	Reporter observability.ReporterFactory
}

// Runtime is what a service's Setup receives.
type Runtime struct {
	Config *config.Chassis
	Log    *slog.Logger
	Level  *slog.LevelVar
	Errors *httpx.ErrorWriter
	Mux    *httpx.Mux
	Health *health.Registry
	Drain  *lifecycle.Drain
	Clock  clock.Clock
	Tracer func(name string) trace.Tracer

	// Internal carries traceparent AND baggage. Control-plane hops only.
	Internal *http.Client
	// Vendor carries traceparent only, with baggage stripped. Use it for
	// Stripe, the ad platforms and the model providers.
	Vendor *http.Client
}

// H adapts a fallible handler.
func (rt *Runtime) H(h httpx.HandlerFunc) http.HandlerFunc { return rt.Errors.H(h) }

// Options configures New. Tests use it to inject a lookup, a log sink, a
// pre-built exporter and a listener.
type Options struct {
	Service    Service
	Lookup     config.Lookup
	Out        io.Writer
	Clock      clock.Clock
	Exporter   sdktrace.SpanExporter
	SetGlobals bool
	Listener   net.Listener
}

// Chassis is an assembled service.
type Chassis struct {
	cfg    *config.Chassis
	log    *slog.Logger
	level  *slog.LevelVar
	tel    *telemetry.Provider
	mux    *httpx.Mux
	health *health.Registry
	drain  *lifecycle.Drain
	srv    *Server
	admin  *AdminServer
	closer io.Closer
	ln     net.Listener
	clk    clock.Clock
}

// New assembles a service.
//
// The order is the design:
//
//  1. config — a bad deploy must die before it takes traffic, reporting every
//     problem at once rather than one per deploy cycle.
//  2. logger — slog.SetDefault, so a third-party library's log.Printf becomes
//     JSON through the redaction Guard.
//  3. telemetry — never blocks on a collector; then error reporting, which is
//     nil unless a service main opted in AND a DSN is bound.
//  4. health registry.
//  5. router, mounting /healthz and /readyz on the root BEFORE any auth.
//  6. service Setup, then service Probes.
//  7. reg.Start, which blocks until the first full probe pass, so the process
//     never reports ready before it knows.
//  8. listener.
func New(ctx context.Context, o Options) (*Chassis, error) {
	svc := o.Service
	if svc.Name == "" {
		return nil, errors.New("chassis: Service.Name is required")
	}
	clk := o.Clock
	if clk == nil {
		clk = clock.Real()
	}
	lookup := o.Lookup
	if lookup == nil {
		lookup = config.OSLookup()
	}

	// 1. config
	loader := config.NewLoader(lookup)
	cfg := &config.Chassis{}
	cfg.Bind(loader, config.Defaults{Service: svc.Name, Version: svc.Version, Commit: svc.Commit})
	if svc.Bind != nil {
		svc.Bind(loader)
	}
	if err := loader.Err(); err != nil {
		return nil, err
	}

	// 2. logging
	format := logging.FormatJSON
	if cfg.LogFormatText() {
		format = logging.FormatText
	}
	handler, level := logging.NewHandler(logging.Config{
		Service: cfg.Service, Env: string(cfg.Env), Version: cfg.Version, Commit: cfg.Commit,
		InstanceID: cfg.InstanceID, Level: cfg.Log.Level, Format: format,
		AddSource: cfg.Log.AddSource, Out: o.Out,
	})
	log := slog.New(handler)
	if o.SetGlobals {
		logging.Install(log, handler)
	}
	log.LogAttrs(ctx, slog.LevelInfo, "configuration resolved", loader.Resolved()...)

	// 3. telemetry
	tel, err := telemetry.Setup(ctx, telemetry.Options{
		Config: telemetry.Config{
			Service: cfg.Service, Version: cfg.Version, Commit: cfg.Commit,
			Env: string(cfg.Env), InstanceID: cfg.InstanceID,
			Endpoint: cfg.OTel.Endpoint, Protocol: cfg.OTel.Protocol, Headers: cfg.OTel.Headers,
			SampleRatio: cfg.OTel.SampleRatio, SampleRatioSet: cfg.OTel.SampleRatioSet,
			ShutdownTimeout: cfg.OTel.ShutdownTimeout,
		},
		Exporter: o.Exporter, Factory: svc.Exporter,
		Log: log, Clock: clk, SetGlobals: o.SetGlobals,
	})
	if err != nil {
		return nil, err
	}

	// 3b. error reporting (SPEC 17.3). Two facts shape this:
	//
	// Nothing is constructed unless a service main opted in AND a DSN resolved,
	// so development, CI and any self-hosted deploy pay nothing — not a
	// goroutine, not a ticker.
	//
	// A bound DSN with no factory is the silent misconfiguration worth a line:
	// an operator sets SENTRY_DSN, sees no error, and gets no issues.
	var reporter observability.Reporter
	if svc.Reporter != nil {
		r, rerr := svc.Reporter(ctx, observability.Config{
			DSN:     cfg.Sentry.DSN,
			Service: cfg.Service, Version: cfg.Version, Commit: cfg.Commit,
			Env: string(cfg.Env), InstanceID: cfg.InstanceID,
		})
		if rerr != nil {
			return nil, rerr
		}
		reporter = r
	} else if cfg.Sentry.DSN != "" {
		log.LogAttrs(ctx, slog.LevelWarn,
			"a Sentry DSN is configured but this binary links no error reporter",
			slog.String(logkey.ConfigKey, config.KeySentryDSN),
			slog.String(logkey.Reason, "set Service.Reporter in the service main"))
	}
	// Registered last in the shutdown sequence's telemetry phase, after the
	// span flush, so the drain's own spans are already gone by the time an
	// issue links to them.
	telemetryShutdown := tel.Shutdown
	var panicHook httpx.PanicHook
	if reporter != nil {
		rep := reporter
		telemetryShutdown = func(sctx context.Context) error {
			return errors.Join(tel.Shutdown(sctx), rep.Shutdown(sctx))
		}
		panicHook = rep.CapturePanic
	}

	// 4. health
	reg := health.New(health.Options{
		Interval: cfg.Readiness.Interval, Timeout: cfg.Readiness.Timeout,
		StaleAfter: cfg.Readiness.StaleAfter, Log: log, Clock: clk,
	})
	drain := lifecycle.NewDrain()
	ew := httpx.NewErrorWriter(log)

	// 5. router
	mux := httpx.Router(httpx.RouterConfig{
		Service: cfg.Service, Env: string(cfg.Env), Version: cfg.Version, Commit: cfg.Commit,
		Log: log, Errors: ew, Health: reg, Drain: drain,
		TracerProvider: tel.TracerProvider(),
		RequestID: httpx.RequestIDConfig{
			TrustInbound: cfg.TrustInboundRequestID, TrustedProxies: cfg.TrustedProxies,
		},
		TrustedProxies:  cfg.TrustedProxies,
		HandlerTimeout:  cfg.HTTP.HandlerTimeout,
		MaxBodyBytes:    cfg.HTTP.MaxBodyBytes,
		ReadinessDetail: cfg.Readiness.Detail,
		PanicHook:       panicHook,
	})

	c := &Chassis{
		cfg: cfg, log: log, level: level, tel: tel, mux: mux,
		health: reg, drain: drain, clk: clk,
	}
	rt := c.Runtime()

	// From here on a failure must not leave the reporter's transport running:
	// New returning an error means the process is about to exit through
	// Main, which never reaches the shutdown sequence.
	abandon := func(err error) (*Chassis, error) {
		if reporter != nil {
			_ = reporter.Shutdown(ctx)
		}
		return nil, err
	}

	// 6. service
	if svc.Setup != nil {
		closer, serr := svc.Setup(ctx, rt)
		if serr != nil {
			return abandon(serr)
		}
		c.closer = closer
	}
	if svc.Probes != nil {
		svc.Probes(rt, reg)
	}

	// 7. readiness: block until the first pass
	if serr := reg.Start(ctx); serr != nil {
		return abandon(serr)
	}

	// 8. listener
	c.srv = NewServer(ServerConfig{
		Addr:              cfg.Addr,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		MaxHeaderBytes:    cfg.HTTP.MaxHeaderBytes,
		MaxHeaderValues:   cfg.HTTP.MaxHeaderValues,
		DeregisterDelay:   cfg.Shutdown.DeregisterDelay,
		DrainTimeout:      cfg.Shutdown.DrainTimeout,
		ServerTimeout:     cfg.Shutdown.ServerTimeout,
		TelemetryTimeout:  cfg.Shutdown.TelemetryTimeout,
	}, log, handler, reg, drain, mux.Handler(), telemetryShutdown, clk)

	if cfg.AdminAddr != "" {
		c.admin = NewAdminServer(cfg.AdminAddr, log, level, reg, loader.Resolved())
	}
	c.ln = o.Listener
	return c, nil
}

// Runtime returns what a service's Setup receives.
func (c *Chassis) Runtime() *Runtime {
	tp := c.tel.TracerProvider()
	return &Runtime{
		Config: c.cfg, Log: c.log, Level: c.level,
		Errors: httpx.NewErrorWriter(c.log), Mux: c.mux,
		Health: c.health, Drain: c.drain, Clock: c.clk,
		Tracer:   func(name string) trace.Tracer { return tp.Tracer(name) },
		Internal: telemetry.Internal(tp, nil),
		Vendor:   telemetry.Vendor(tp, nil),
	}
}

// Handler gives the full production middleware stack to httptest, with no
// listener — so an end-to-end test exercises the real stack.
func (c *Chassis) Handler() http.Handler { return c.mux.Handler() }

// Addr returns the resolved listen address.
func (c *Chassis) Addr() string { return c.srv.Addr() }

// Ready closes once the server is accepting.
func (c *Chassis) Ready() <-chan struct{} { return c.srv.Ready() }

// Serve runs until ctx is cancelled, then shuts down.
func (c *Chassis) Serve(ctx context.Context) (lifecycle.Report, error) {
	if c.admin != nil {
		go func() {
			if err := c.admin.Run(ctx); err != nil {
				c.log.LogAttrs(ctx, slog.LevelWarn, "admin listener stopped",
					slog.String(logkey.Err, err.Error()))
			}
		}()
	}

	var rep lifecycle.Report
	var err error
	if c.ln != nil {
		rep, err = c.srv.Serve(ctx, c.ln)
	} else {
		rep, err = c.srv.Run(ctx)
	}

	// The service's Closer runs AFTER the drain, so a pool closes only once no
	// handler can still reach it.
	if c.closer != nil {
		if cerr := c.closer.Close(); cerr != nil {
			c.log.LogAttrs(ctx, slog.LevelWarn, "closing service dependencies",
				slog.String(logkey.Err, cerr.Error()))
		}
	}
	return rep, err
}

// Run is New followed by Serve.
func Run(ctx context.Context, s Service, get config.Lookup) error {
	c, err := New(ctx, Options{Service: s, Lookup: get, SetGlobals: true})
	if err != nil {
		return err
	}
	_, serveErr := c.Serve(ctx)
	return serveErr
}

// Main is Run plus signal handling, the -version flag and the exit code.
func Main(s Service) {
	printVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *printVersion {
		fmt.Printf("%s %s (%s)\n", s.Name, s.Version, s.Commit)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := Run(ctx, s, config.OSLookup()); err != nil {
		if cfgErr, ok := config.AsError(err); ok {
			// Every problem, so a crash-looping container's last line names
			// every variable to fix.
			fmt.Fprintln(os.Stderr, cfgErr.Error())
			os.Exit(ExitConfig)
		}
		fmt.Fprintf(os.Stderr, "%s: %v\n", s.Name, err)
		os.Exit(1)
	}
}
