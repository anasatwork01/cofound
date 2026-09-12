package config

import (
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"time"
)

// Env is the deployment environment. Several defaults and two refusals depend
// on it, so it is typed rather than a bare string.
type Env string

const (
	EnvDevelopment Env = "development"
	EnvStaging     Env = "staging"
	EnvProduction  Env = "production"
)

// IsProduction reports whether the stricter rules apply.
func (e Env) IsProduction() bool { return e == EnvProduction }

// Valid reports whether e is a known environment.
func (e Env) Valid() bool {
	switch e {
	case EnvDevelopment, EnvStaging, EnvProduction:
		return true
	}
	return false
}

// Chassis is the configuration every service shares.
//
// OTel keys use the OTel specification's own names rather than a HALYARD_OTLP_*
// spelling: the SDK reads some of them itself, and an operator who already
// knows OTel should not have to learn our synonyms.
type Chassis struct {
	Service    string
	Version    string
	Commit     string
	InstanceID string
	Env        Env

	Addr      string
	AdminAddr string

	Log struct {
		Level     slog.Level
		Format    string
		AddSource bool
	}

	OTel struct {
		Endpoint        string
		Protocol        string
		Headers         map[string]string
		SampleRatio     float64
		SampleRatioSet  bool
		ShutdownTimeout time.Duration
	}

	// Sentry is SPEC 17.3's error reporting. DSN empty means no reporter, and
	// that is the ONLY switch: see observability/sentry.Init for why an empty
	// DSN must never reach sentry-go.
	Sentry struct {
		DSN string
	}

	HTTP struct {
		ReadHeaderTimeout time.Duration
		ReadTimeout       time.Duration
		WriteTimeout      time.Duration
		IdleTimeout       time.Duration
		HandlerTimeout    time.Duration
		MaxHeaderBytes    int
		MaxHeaderValues   int
		MaxBodyBytes      int64
	}

	Shutdown struct {
		DeregisterDelay  time.Duration
		DrainTimeout     time.Duration
		ServerTimeout    time.Duration
		TelemetryTimeout time.Duration
		Budget           time.Duration
	}

	Readiness struct {
		Interval   time.Duration
		Timeout    time.Duration
		StaleAfter time.Duration
		Detail     bool
	}

	TrustedProxies        []netip.Prefix
	TrustInboundRequestID bool
}

// Defaults carries build metadata from package main, where the Makefile's
// -X main.version / -X main.commit land.
type Defaults struct {
	Service string
	Version string
	Commit  string
}

const (
	KeyEnv        = "HALYARD_ENV"
	KeyInstanceID = "INSTANCE_ID"
	KeyAddr       = "HTTP_ADDR"
	KeyAdminAddr  = "ADMIN_ADDR"

	KeyLogLevel  = "LOG_LEVEL"
	KeyLogFormat = "LOG_FORMAT"
	KeyLogSource = "LOG_SOURCE"

	// SENTRY_DSN keeps its vendor spelling for the same reason the OTLP keys
	// do: an operator pasting a DSN out of the Sentry UI should not have to
	// learn a Halyard synonym. Binding it here rather than letting sentry-go
	// read the environment itself is what makes it appear in the boot line and
	// in config validation like every other key.
	KeySentryDSN = "SENTRY_DSN"

	KeyOTLPEndpoint     = "OTEL_EXPORTER_OTLP_ENDPOINT"
	KeyOTLPProtocol     = "OTEL_EXPORTER_OTLP_PROTOCOL"
	KeyOTLPHeaders      = "OTEL_EXPORTER_OTLP_HEADERS"
	KeyOTelSamplerArg   = "OTEL_TRACES_SAMPLER_ARG"
	KeyOTelShutdown     = "OTEL_SHUTDOWN_TIMEOUT"
	KeyReadHeaderTO     = "HTTP_READ_HEADER_TIMEOUT"
	KeyReadTO           = "HTTP_READ_TIMEOUT"
	KeyWriteTO          = "HTTP_WRITE_TIMEOUT"
	KeyIdleTO           = "HTTP_IDLE_TIMEOUT"
	KeyHandlerTO        = "HTTP_HANDLER_TIMEOUT"
	KeyMaxHeaderBytes   = "HTTP_MAX_HEADER_BYTES"
	KeyMaxHeaderValues  = "HTTP_MAX_HEADER_VALUES"
	KeyMaxBodyBytes     = "HTTP_MAX_BODY_BYTES"
	KeyDeregisterDelay  = "SHUTDOWN_DEREGISTER_DELAY"
	KeyDrainTimeout     = "SHUTDOWN_DRAIN_TIMEOUT"
	KeyServerTimeout    = "SHUTDOWN_SERVER_TIMEOUT"
	KeyTelemetryTimeout = "SHUTDOWN_TELEMETRY_TIMEOUT"
	KeyShutdownBudget   = "SHUTDOWN_BUDGET"

	KeyReadinessInterval   = "READINESS_INTERVAL"
	KeyReadinessTimeout    = "READINESS_TIMEOUT"
	KeyReadinessStaleAfter = "READINESS_STALE_AFTER"
	KeyReadinessDetail     = "READINESS_DETAIL"

	KeyTrustedProxies = "TRUSTED_PROXY_CIDRS"
	KeyTrustRequestID = "TRUST_INBOUND_REQUEST_ID"
)

// Bind reads every chassis key on l.
//
// A service calls Bind, binds its own fields on the same Loader, then calls
// l.Err() once — so one boot reports the union of chassis and service problems
// rather than making an operator fix them one deploy at a time.
func (c *Chassis) Bind(l *Loader, d Defaults) {
	c.Service = d.Service
	c.Version = d.Version
	c.Commit = d.Commit

	c.Env = Env(l.Enum(KeyEnv, string(EnvDevelopment),
		string(EnvDevelopment), string(EnvStaging), string(EnvProduction)))
	c.InstanceID = l.String(KeyInstanceID, "")

	c.Addr = l.String(KeyAddr, ":8080")
	c.AdminAddr = l.String(KeyAdminAddr, "")

	c.Log.Level = l.Level(KeyLogLevel, slog.LevelInfo)
	c.Log.Format = l.Enum(KeyLogFormat, "json", "json", "text")
	c.Log.AddSource = l.Bool(KeyLogSource, !c.Env.IsProduction())

	c.OTel.Endpoint = l.String(KeyOTLPEndpoint, "")
	c.OTel.Protocol = l.Enum(KeyOTLPProtocol, "http/protobuf", "http/protobuf", "grpc")
	c.OTel.Headers = l.KeyValues(KeyOTLPHeaders)
	c.OTel.SampleRatio, c.OTel.SampleRatioSet = l.OptionalFloat(KeyOTelSamplerArg, 0, 1)
	c.OTel.ShutdownTimeout = l.Duration(KeyOTelShutdown, 5*time.Second, time.Second, time.Minute)

	// Validated as a URL here rather than at Init, so a typo is one of the
	// problems the boot line reports alongside every other bad variable instead
	// of a separate crash. Fingerprinted rather than echoed: the userinfo is a
	// write-only ingest key, and there is no diagnostic that needs the value
	// when "set (sha256:...)" already tells an operator whether two replicas
	// agree.
	if u := l.SecretURL(KeySentryDSN, false, "https", "http"); u != nil {
		c.Sentry.DSN = u.String()
	}

	c.HTTP.ReadHeaderTimeout = l.Duration(KeyReadHeaderTO, 5*time.Second, time.Second, time.Minute)
	c.HTTP.ReadTimeout = l.Duration(KeyReadTO, 30*time.Second, time.Second, 10*time.Minute)
	// Stays non-zero: it protects every JSON endpoint from a slow client. SSE
	// clears it per-request in httpx.Open, because only WriteTimeout cuts a
	// live stream and only the stream knows it is one.
	c.HTTP.WriteTimeout = l.Duration(KeyWriteTO, 30*time.Second, 0, 10*time.Minute)
	c.HTTP.IdleTimeout = l.Duration(KeyIdleTO, 120*time.Second, time.Second, 30*time.Minute)
	c.HTTP.HandlerTimeout = l.Duration(KeyHandlerTO, 15*time.Second, time.Second, 5*time.Minute)
	c.HTTP.MaxHeaderBytes = l.Bytes(KeyMaxHeaderBytes, 1<<20)
	c.HTTP.MaxHeaderValues = l.Int(KeyMaxHeaderValues, 500, 1, 100_000)
	c.HTTP.MaxBodyBytes = int64(l.Bytes(KeyMaxBodyBytes, 1<<20))

	// Zero in development: waiting three seconds for a load balancer that is
	// not there makes every local restart feel broken.
	deregisterDefault := 3 * time.Second
	if c.Env == EnvDevelopment {
		deregisterDefault = 0
	}
	c.Shutdown.DeregisterDelay = l.Duration(KeyDeregisterDelay, deregisterDefault, 0, time.Minute)
	c.Shutdown.DrainTimeout = l.Duration(KeyDrainTimeout, 10*time.Second, 0, 10*time.Minute)
	c.Shutdown.ServerTimeout = l.Duration(KeyServerTimeout, 5*time.Second, time.Second, 5*time.Minute)
	c.Shutdown.TelemetryTimeout = l.Duration(KeyTelemetryTimeout, 5*time.Second, time.Second, time.Minute)
	c.Shutdown.Budget = l.Duration(KeyShutdownBudget, 25*time.Second, time.Second, 15*time.Minute)

	c.Readiness.Interval = l.Duration(KeyReadinessInterval, 2*time.Second, 100*time.Millisecond, time.Minute)
	c.Readiness.Timeout = l.Duration(KeyReadinessTimeout, time.Second, 50*time.Millisecond, time.Minute)
	c.Readiness.StaleAfter = l.Duration(KeyReadinessStaleAfter, 10*time.Second, time.Second, 10*time.Minute)
	c.Readiness.Detail = l.Bool(KeyReadinessDetail, false)

	c.TrustedProxies = l.CIDRs(KeyTrustedProxies)
	c.TrustInboundRequestID = l.Bool(KeyTrustRequestID, false)

	c.validate(l)
}

// validate holds the cross-field rules. They live in ordinary Go because no tag
// DSL expresses "refuse LOG_LEVEL=debug when HALYARD_ENV=production".
func (c *Chassis) validate(l *Loader) {
	// 1. Debug logging is where user content is revealed (SPEC 17.3), so it is
	//    a privacy setting in production, not a verbosity one.
	if c.Env.IsProduction() && c.Log.Level <= slog.LevelDebug {
		l.Problemf(KeyLogLevel, "expect info or higher when %s=production; debug reveals user content (SPEC 17.3)", KeyEnv)
	}

	// 2. The phases must fit inside the orchestrator's termination grace
	//    period. Overrunning it means SIGKILL mid-write, which is exactly the
	//    unclean cut that Last-Event-ID resume exists to make unnecessary.
	sum := c.Shutdown.DeregisterDelay + c.Shutdown.DrainTimeout +
		c.Shutdown.ServerTimeout + c.Shutdown.TelemetryTimeout
	if sum > c.Shutdown.Budget {
		l.Problemf("", "shutdown phases total %s but %s is %s; lower %s, %s, %s or %s, or raise the budget",
			sum, KeyShutdownBudget, c.Shutdown.Budget,
			KeyDeregisterDelay, KeyDrainTimeout, KeyServerTimeout, KeyTelemetryTimeout)
	}

	// 3. An idle timeout below the header timeout closes keep-alive connections
	//    before a slow client finishes its headers.
	if c.HTTP.IdleTimeout <= c.HTTP.ReadHeaderTimeout {
		l.Problemf(KeyIdleTO, "expect a value greater than %s (%s)", KeyReadHeaderTO, c.HTTP.ReadHeaderTimeout)
	}

	// 4. A stale window under two intervals marks healthy probes unknown on a
	//    single slow poll, which reads as a flapping outage.
	if c.Readiness.StaleAfter <= 2*c.Readiness.Interval {
		l.Problemf(KeyReadinessStaleAfter, "expect a value greater than twice %s (%s)",
			KeyReadinessInterval, 2*c.Readiness.Interval)
	}
	if c.Readiness.Timeout >= c.Readiness.Interval {
		l.Problemf(KeyReadinessTimeout, "expect a value below %s (%s)", KeyReadinessInterval, c.Readiness.Interval)
	}

	// 5. pprof, the resolved configuration and the log-level knob are not
	//    public surfaces.
	if c.Env.IsProduction() && c.AdminAddr != "" && !isLoopbackOrPrivate(c.AdminAddr) {
		l.Problemf(KeyAdminAddr, "expect a loopback or private address in production; it exposes pprof and the log-level control")
	}

	// 6. /readyz is unauthenticated, and detail includes probe error strings.
	if c.Env.IsProduction() && c.Readiness.Detail {
		l.Problemf(KeyReadinessDetail, "expect false in production; /readyz is unauthenticated and detail includes error strings")
	}
}

func isLoopbackOrPrivate(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" {
		// A bare ":9090" binds every interface.
		return false
	}
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		// A hostname we cannot classify; assume it is routable.
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// Load reads the chassis configuration and reports every problem at once.
func Load(get Lookup, d Defaults) (*Chassis, error) {
	l := NewLoader(get)
	var c Chassis
	c.Bind(l, d)
	if err := l.Err(); err != nil {
		return nil, err
	}
	return &c, nil
}

// LogFormatText reports whether the text handler was requested.
func (c *Chassis) LogFormatText() bool { return strings.EqualFold(c.Log.Format, "text") }
