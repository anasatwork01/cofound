package chassis_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis"
	"github.com/anasatwork01/cofound/packages/chassis/config"
	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/chassis/health"
	"github.com/anasatwork01/cofound/packages/chassis/httpx"
	"github.com/anasatwork01/cofound/packages/chassis/logging"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/common"
)

func boot(t *testing.T, env map[string]string, svc chassis.Service) (*chassis.Chassis, *logging.Sink) {
	t.Helper()
	if env == nil {
		env = map[string]string{}
	}
	if _, ok := env["HALYARD_ENV"]; !ok {
		env["HALYARD_ENV"] = "development"
	}
	sink := logging.NewSink()
	if svc.Name == "" {
		svc.Name = "api"
	}
	c, err := chassis.New(context.Background(), chassis.Options{
		Service: svc, Lookup: config.MapLookup(env), Out: sink,
	})
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	return c, sink
}

// TestConfigurationFailureReportsEveryProblemAtOnce. Reporting the first
// problem only costs one deploy cycle per bad variable, which is the whole
// reason the loader accumulates.
func TestConfigurationFailureReportsEveryProblemAtOnce(t *testing.T) {
	t.Parallel()
	_, err := chassis.New(context.Background(), chassis.Options{
		Service: chassis.Service{Name: "api"},
		Lookup: config.MapLookup(map[string]string{
			"HALYARD_ENV":       "production",
			"LOG_LEVEL":         "debug", // refused in production: reveals user content
			"HTTP_READ_TIMEOUT": "not-a-duration",
			"READINESS_DETAIL":  "true", // refused in production: /readyz is public
			"HTTP_IDLE_TIMEOUT": "1s",   // must exceed the header timeout
		}),
		Out: io.Discard,
	})
	if err == nil {
		t.Fatal("expected a configuration error")
	}
	cfgErr, ok := config.AsError(err)
	if !ok {
		t.Fatalf("error is not a *config.Error: %T", err)
	}
	if len(cfgErr.Problems) < 4 {
		t.Errorf("reported %d problems, want at least 4:\n%s", len(cfgErr.Problems), err)
	}
	msg := err.Error()
	for _, key := range []string{"LOG_LEVEL", "HTTP_READ_TIMEOUT", "READINESS_DETAIL", "HTTP_IDLE_TIMEOUT"} {
		if !strings.Contains(msg, key) {
			t.Errorf("the message does not name %s:\n%s", key, msg)
		}
	}
}

// TestConfigProblemsNeverEchoTheValue. A configuration error is logged, and a
// malformed DATABASE_URL echoed into that log is a credential in a log file.
func TestConfigProblemsNeverEchoTheValue(t *testing.T) {
	t.Parallel()
	const bad = "postgres://user:s3cr3t@host/db?bad"
	l := config.NewLoader(config.MapLookup(map[string]string{"X_DURATION": bad}))
	l.Duration("X_DURATION", time.Second, 0, time.Minute)
	err := l.Err()
	if err == nil {
		t.Fatal("expected a problem")
	}
	if strings.Contains(err.Error(), "s3cr3t") {
		t.Fatalf("the problem echoed the value:\n%s", err)
	}
}

// TestShutdownBudgetIsEnforced. If the phases outlive the orchestrator's grace
// period the process is SIGKILLed mid-write.
func TestShutdownBudgetIsEnforced(t *testing.T) {
	t.Parallel()
	_, err := chassis.New(context.Background(), chassis.Options{
		Service: chassis.Service{Name: "api"},
		Lookup: config.MapLookup(map[string]string{
			"SHUTDOWN_DRAIN_TIMEOUT": "120s",
			"SHUTDOWN_BUDGET":        "25s",
		}),
		Out: io.Discard,
	})
	if err == nil {
		t.Fatal("expected the budget rule to reject this")
	}
	if !strings.Contains(err.Error(), "SHUTDOWN_BUDGET") {
		t.Errorf("the message should name the budget:\n%s", err)
	}
}

// TestHealthAndReadinessThroughTheRealStack.
func TestHealthAndReadinessThroughTheRealStack(t *testing.T) {
	t.Parallel()
	down := make(chan struct{})
	c, _ := boot(t, nil, chassis.Service{
		Name: "api", Version: "1.2.3", Commit: "abc1234",
		Probes: func(rt *chassis.Runtime, reg *health.Registry) {
			reg.Register(health.PingFunc("postgres", func(context.Context) error {
				select {
				case <-down:
					return errContrived
				default:
					return nil
				}
			}, health.Critical))
		},
	})

	srv := &http.Server{Handler: c.Handler()}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	base := "http://" + ln.Addr().String()

	// Liveness carries build metadata, which is how you tell which version is
	// answering during a rollout.
	resp, body := get(t, base+"/healthz")
	if resp.StatusCode != 200 {
		t.Fatalf("healthz = %d", resp.StatusCode)
	}
	for _, want := range []string{`"service":"api"`, `"version":"1.2.3"`, `"commit":"abc1234"`} {
		if !strings.Contains(body, want) {
			t.Errorf("healthz body missing %s: %s", want, body)
		}
	}

	if resp, _ := get(t, base+"/readyz"); resp.StatusCode != 200 {
		t.Errorf("readyz = %d, want 200 while the probe is up", resp.StatusCode)
	}
}

// TestUnknownRouteReturnsTheEnvelopeThroughTheRealStack, because chi's default
// would be text/plain that every generated client decoder fails to parse.
func TestUnknownRouteReturnsTheEnvelope(t *testing.T) {
	t.Parallel()
	c, _ := boot(t, nil, chassis.Service{Name: "api"})
	srv := &http.Server{Handler: c.Handler()}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	resp, body := get(t, "http://"+ln.Addr().String()+"/v1/nope")
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var env common.ErrorResponse
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("not a decodable envelope: %v\n%s", err, body)
	}
	if env.Error.Code != "not_found" {
		t.Errorf("code = %q", env.Error.Code)
	}
}

// TestServiceRoutesGetTheFullStack proves Setup can mount a handler and that a
// returned error renders from the catalogue.
func TestServiceRoutesGetTheFullStack(t *testing.T) {
	t.Parallel()
	c, sink := boot(t, nil, chassis.Service{
		Name: "api",
		Setup: func(ctx context.Context, rt *chassis.Runtime) (io.Closer, error) {
			rt.Mux.Public.Get("/v1/boom", rt.H(func(w http.ResponseWriter, r *http.Request) error {
				return errs.Forbidden("publish this project")
			}))
			rt.Mux.Public.Get("/v1/ok", rt.H(func(w http.ResponseWriter, r *http.Request) error {
				return httpx.JSON(w, 200, map[string]string{"ok": "yes"})
			}))
			return nil, nil
		},
	})
	srv := &http.Server{Handler: c.Handler()}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	base := "http://" + ln.Addr().String()

	resp, body := get(t, base+"/v1/boom")
	if resp.StatusCode != 403 {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	var env common.ErrorResponse
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q", env.Error.Code)
	}
	if env.Error.Fix == nil || *env.Error.Fix == "" {
		t.Error("SPEC 18 requires errors to say how to fix them")
	}
	if !strings.Contains(env.Error.Message, "publish this project") {
		t.Errorf("message = %q", env.Error.Message)
	}

	if resp, _ := get(t, base+"/v1/ok"); resp.StatusCode != 200 {
		t.Errorf("ok route = %d", resp.StatusCode)
	}

	// Every request produced an access log line with the ROUTE, not the path.
	var sawRoute bool
	for _, line := range sink.Lines() {
		if line["msg"] == "http request" && line["http_route"] == "/v1/boom" {
			sawRoute = true
		}
	}
	if !sawRoute {
		t.Errorf("no access log line carried the route pattern:\n%s", sink.String())
	}
}

// TestBootsWithoutACollector, which is what keeps local development and CI
// usable given OTEL_SDK_DISABLED is unimplemented in the Go SDK.
func TestBootsWithoutACollector(t *testing.T) {
	t.Parallel()
	c, sink := boot(t, nil, chassis.Service{Name: "api"})
	if c.Handler() == nil {
		t.Fatal("no handler")
	}
	if strings.Contains(sink.String(), "connection refused") {
		t.Error("boot tried to reach a collector")
	}
}

// TestSecretConfigIsFingerprintedNotLogged. The boot line records effective
// configuration, which must be enough to compare two replicas and never enough
// to authenticate.
func TestSecretConfigIsFingerprintedNotLogged(t *testing.T) {
	t.Parallel()
	const dsn = "postgres://halyard:s3cr3t@db.internal:5432/halyard"
	_, sink := boot(t, map[string]string{"DATABASE_URL": dsn}, chassis.Service{
		Name: "api",
		Bind: func(l *config.Loader) { l.SecretURL("DATABASE_URL", false, "postgres", "postgresql") },
	})
	if sink.Contains("s3cr3t") {
		t.Fatalf("the boot line leaked a credential:\n%s", sink.String())
	}
	if !sink.Contains("sha256:") {
		t.Errorf("expected a fingerprint so two replicas can be compared:\n%s", sink.String())
	}
}

var errContrived = contrivedError{}

type contrivedError struct{}

func (contrivedError) Error() string { return "contrived failure" }

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}
