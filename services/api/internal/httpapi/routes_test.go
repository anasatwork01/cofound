package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/anasatwork01/cofound/packages/chassis"
	"github.com/anasatwork01/cofound/packages/chassis/config"
	"github.com/anasatwork01/cofound/packages/chassis/logging"
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

// TestReadyzIsReadyOnceDependenciesAreConfigured, so the negative test above
// is not passing for an unrelated reason.
func TestReadyzIsReadyOnceConfigured(t *testing.T) {
	t.Parallel()
	base, _ := boot(t, map[string]string{
		"DATABASE_URL": "postgres://halyard:halyard@localhost:55432/halyard",
		"REDIS_URL":    "redis://localhost:56379/0",
	})
	resp, body := get(t, base+"/readyz")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readyz = %d, want 200: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"status":"ready"`) {
		t.Errorf("body = %s", body)
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
