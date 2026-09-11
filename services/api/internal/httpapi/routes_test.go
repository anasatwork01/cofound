package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/anasatwork01/cofound/packages/chassis"
	"github.com/anasatwork01/cofound/packages/chassis/config"
	"github.com/anasatwork01/cofound/packages/chassis/logging"
	"github.com/anasatwork01/cofound/packages/db"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/common"

	"github.com/anasatwork01/cofound/services/api/internal/httpapi"
)

// boot assembles the real api service over a real listener, per working
// agreement 3: one end-to-end test per user-visible flow.
func boot(t *testing.T, env map[string]string) (string, *logging.Sink) {
	t.Helper()
	if env == nil {
		env = map[string]string{}
	}
	env["HALYARD_ENV"] = "development"
	sink := logging.NewSink()

	api := httpapi.New()
	// Boot without Postgres. These tests assert on routing, the error envelope
	// and the boot line; none of them issue a query, and requiring a live
	// database would mean either adding Postgres to the `go` CI job or skipping
	// them by default. The tenant isolation the pool actually guards is proven
	// in tests/integration/test_tenancy.py and packages/db, against a real
	// database and as the unprivileged role -- which is the only place it can
	// be proven at all.
	if env["DATABASE_URL"] == "" {
		env["DATABASE_URL"] = "postgres://halyard_app:x@db.invalid:5432/halyard"
	}
	// Required as of task 0.7: every sign-in link is built from it rather than
	// from the request's Host header.
	if env["CONSOLE_ORIGIN"] == "" {
		env["CONSOLE_ORIGIN"] = "http://console.test"
	}
	api.Open = func(context.Context, db.Config) (*db.Pool, error) { return nil, nil }

	c, err := chassis.New(context.Background(), chassis.Options{
		Service: chassis.Service{
			Name: "api", Version: "test", Commit: "test",
			Bind: api.Bind, Setup: api.Setup, Probes: api.Probes,
		},
		Lookup: config.MapLookup(env), Out: sink,
	})
	if err != nil {
		t.Fatalf("boot: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: c.Handler()}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + ln.Addr().String(), sink
}

// TestHealthzIsAliveWithoutDependencies. Liveness must not depend on Postgres,
// or one blip makes the orchestrator restart every replica at once.
func TestHealthzIsAliveWithoutDependencies(t *testing.T) {
	t.Parallel()
	base, _ := boot(t, nil) // no DATABASE_URL, no REDIS_URL

	resp, body := get(t, base+"/healthz")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d, want 200 with no dependencies configured", resp.StatusCode)
	}
	if !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("body = %s", body)
	}
}

// TestReadyzIs503WhenAProbeIsDown, and names it.
func TestReadyzIs503WhenAProbeIsDown(t *testing.T) {
	t.Parallel()
	base, _ := boot(t, nil)

	resp, body := get(t, base+"/readyz")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("readyz = %d, want 503 with unconfigured dependencies", resp.StatusCode)
	}
	for _, want := range []string{"postgres", "redis", `"status":"unready"`} {
		if !strings.Contains(body, want) {
			t.Errorf("readyz body missing %q: %s", want, body)
		}
	}
}

// TestReadyzNamesProbesWithoutErrorStrings. /readyz is unauthenticated, and a
// probe error routinely carries a hostname, a port or a driver message.
func TestReadyzNamesProbesWithoutErrorStrings(t *testing.T) {
	t.Parallel()
	base, _ := boot(t, nil)
	_, body := get(t, base+"/readyz")

	if strings.Contains(body, "is not configured") {
		t.Errorf("a probe error string reached an unauthenticated endpoint:\n%s", body)
	}
}

// TestReadyzIsReadyAgainstARealDatabase, so the negative test above is not
// passing for an unrelated reason.
//
// The contract changed in task 0.6 and this test changed with it. Readiness
// used to mean "DATABASE_URL is set", which is a check on a string; it now
// means "the database answered `select 1`", which is a check on the thing that
// matters. The consequence is that this needs a real database, so it skips
// without one rather than asserting something weaker.
func TestReadyzIsReadyAgainstARealDatabase(t *testing.T) {
	t.Parallel()
	appURL := os.Getenv("APP_DATABASE_URL")
	if appURL == "" {
		if os.Getenv("CI") != "" && os.Getenv("DATABASE_URL") != "" {
			// A skip here in the integration job would be a false green.
			t.Fatal("APP_DATABASE_URL is unset but DATABASE_URL is set; the harness is misconfigured")
		}
		t.Skip("APP_DATABASE_URL unset; run `make db-setup`")
	}

	sink := logging.NewSink()
	api := httpapi.New() // the real db.Open, deliberately
	c, err := chassis.New(context.Background(), chassis.Options{
		Service: chassis.Service{
			Name: "api", Version: "test", Commit: "test",
			Bind: api.Bind, Setup: api.Setup, Probes: api.Probes,
		},
		Lookup: config.MapLookup(map[string]string{
			"HALYARD_ENV":    "development",
			"DATABASE_URL":   appURL,
			"REDIS_URL":      "redis://localhost:56379/0",
			"CONSOLE_ORIGIN": "http://console.test",
		}),
		Out: sink,
	})
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	// Chassis has no Close -- shutdown runs through Serve, which this test does
	// not use -- so the pool is closed directly. Without this the test leaks a
	// real connection pool for the rest of the run.
	t.Cleanup(func() {
		if api.DB != nil {
			api.DB.Close()
		}
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: c.Handler()}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	resp, body := get(t, "http://"+ln.Addr().String()+"/readyz")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readyz = %d, want 200: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"status":"ready"`) {
		t.Errorf("body = %s", body)
	}
}

// TestSetupRefusesAPrivilegedDatabaseRole. The service must not start with the
// migration credentials: row-level security is inert for a superuser, so it
// would serve traffic with no tenant isolation and no symptom.
func TestSetupRefusesAPrivilegedDatabaseRole(t *testing.T) {
	t.Parallel()
	ownerURL := os.Getenv("DATABASE_URL")
	if ownerURL == "" {
		t.Skip("DATABASE_URL unset; run `make services-up`")
	}

	api := httpapi.New() // the real db.Open
	_, err := chassis.New(context.Background(), chassis.Options{
		Service: chassis.Service{
			Name: "api", Version: "test", Commit: "test",
			Bind: api.Bind, Setup: api.Setup, Probes: api.Probes,
		},
		Lookup: config.MapLookup(map[string]string{
			"HALYARD_ENV":    "development",
			"DATABASE_URL":   ownerURL,
			"REDIS_URL":      "redis://localhost:56379/0",
			"CONSOLE_ORIGIN": "http://console.test",
		}),
		Out: logging.NewSink(),
	})
	if err == nil {
		t.Fatal("the service booted with a role that bypasses row-level security")
	}
	if !errors.Is(err, db.ErrPrivilegedRole) {
		t.Fatalf("want ErrPrivilegedRole, got %v", err)
	}
}

// TestUnknownRouteReturnsTheErrorEnvelope. api mounts no /v1 routes yet, so
// this is the whole surface — and it must still be a decodable envelope.
func TestUnknownRouteReturnsTheErrorEnvelope(t *testing.T) {
	t.Parallel()
	base, _ := boot(t, nil)

	resp, body := get(t, base+"/v1/projects")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var env common.ErrorResponse
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("the generated decoder rejected it: %v\n%s", err, body)
	}
	if env.Error.Code != "not_found" {
		t.Errorf("code = %q", env.Error.Code)
	}
	if env.Error.RequestId == nil || *env.Error.RequestId == "" {
		t.Error("the envelope must carry a request id for support")
	}
}

// TestDatabaseURLIsFingerprintedInTheBootLine. The boot line records effective
// configuration; it must never be enough to authenticate with.
func TestDatabaseURLIsFingerprintedInTheBootLine(t *testing.T) {
	t.Parallel()
	_, sink := boot(t, map[string]string{
		"DATABASE_URL": "postgres://halyard:s3cr3t@db.internal:5432/halyard",
	})
	if sink.Contains("s3cr3t") {
		t.Fatalf("the api boot line leaked the database password:\n%s", sink.String())
	}
	if !sink.Contains("sha256:") {
		t.Errorf("expected a fingerprint so two replicas can be compared:\n%s", sink.String())
	}
}

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
