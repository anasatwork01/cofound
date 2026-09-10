// Package chassis assembles the shared service runtime: configuration,
// logging, tracing, health, HTTP and shutdown.
//
// A service main calls Main with a Service description and gets all of it. Boot
// ORDER is the design, not an implementation detail — see New.
package chassis

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/clock"
	"github.com/anasatwork01/cofound/packages/chassis/health"
	"github.com/anasatwork01/cofound/packages/chassis/httpx"
	"github.com/anasatwork01/cofound/packages/chassis/lifecycle"
	"github.com/anasatwork01/cofound/packages/chassis/logging"
	"github.com/anasatwork01/cofound/packages/chassis/logkey"
)

// ServerConfig mirrors the HTTP and shutdown halves of config.Chassis.
type ServerConfig struct {
	Addr string

	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
	MaxHeaderValues   int

	DeregisterDelay  time.Duration
	DrainTimeout     time.Duration
	ServerTimeout    time.Duration
	TelemetryTimeout time.Duration
}

// Server owns the listener and the shutdown sequence.
type Server struct {
	srv   *http.Server
	log   *slog.Logger
	seq   *lifecycle.Sequence
	drain *lifecycle.Drain

	addr      string
	readyOnce sync.Once
	ready     chan struct{}
}

// NewServer builds the HTTP server.
func NewServer(
	cfg ServerConfig,
	log *slog.Logger,
	handlerLog slog.Handler,
	reg *health.Registry,
	drain *lifecycle.Drain,
	handler http.Handler,
	telemetryShutdown func(context.Context) error,
	clk clock.Clock,
) *Server {
	if clk == nil {
		clk = clock.Real()
	}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		// Non-zero, protecting every JSON endpoint from a slow client. SSE
		// clears it per request in httpx.Open, because only the stream knows
		// it is one.
		WriteTimeout:   cfg.WriteTimeout,
		IdleTimeout:    cfg.IdleTimeout,
		MaxHeaderBytes: cfg.MaxHeaderBytes,
		// Go 1.27. Legal only because this module declares go 1.27.1: `go vet`
		// rejects it from a go 1.25.0 module even though `go build` accepts it.
		MaxHeaderValueCount: cfg.MaxHeaderValues,
		// net/http's own errors — TLS handshake failures, malformed requests —
		// would otherwise bypass the logging pipeline as bare stderr.
		ErrorLog: logging.StdLogger(handlerLog, slog.LevelWarn),
		// Every request carries the drain, so httpx.Open needs no middleware.
		BaseContext: func(net.Listener) context.Context {
			return httpx.WithDrain(context.Background(), drain)
		},
	}

	return &Server{
		srv: srv, log: log, drain: drain, ready: make(chan struct{}),
		seq: &lifecycle.Sequence{
			Server: srv, Drain: drain, Readiness: reg, Clock: clk, Log: log,
			Telemetry:        telemetryShutdown,
			DeregisterDelay:  cfg.DeregisterDelay,
			DrainTimeout:     cfg.DrainTimeout,
			ServerTimeout:    cfg.ServerTimeout,
			TelemetryTimeout: cfg.TelemetryTimeout,
		},
	}
}

// Addr returns the resolved address, which matters when binding ":0".
func (s *Server) Addr() string { return s.addr }

// Ready closes once the server is accepting connections.
func (s *Server) Ready() <-chan struct{} { return s.ready }

// Serve runs on ln until ctx is cancelled, then shuts down.
func (s *Server) Serve(ctx context.Context, ln net.Listener) (lifecycle.Report, error) {
	s.addr = ln.Addr().String()
	s.log.LogAttrs(ctx, slog.LevelInfo, "listening", slog.String(logkey.Addr, s.addr))
	s.readyOnce.Do(func() { close(s.ready) })

	serveErr := make(chan error, 1)
	go func() {
		err := s.srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	select {
	case err := <-serveErr:
		// The listener died on its own; there is nothing to drain.
		return lifecycle.Report{}, err
	case <-ctx.Done():
		return s.seq.Run(ctx), nil
	}
}

// Run listens on the configured address and serves.
func (s *Server) Run(ctx context.Context) (lifecycle.Report, error) {
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return lifecycle.Report{}, err
	}
	return s.Serve(ctx, ln)
}
