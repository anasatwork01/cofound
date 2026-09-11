package chassis_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/anasatwork01/cofound/packages/chassis"
	"github.com/anasatwork01/cofound/packages/chassis/config"
	"github.com/anasatwork01/cofound/packages/chassis/lifecycle"
	"github.com/anasatwork01/cofound/packages/chassis/observability"
)

// testDSN is syntactically valid and points at a reserved TLD, so nothing here
// can reach a real ingest endpoint even if a real client were built.
const testDSN = "https://0123456789abcdef0123456789abcdef@o0.ingest.example.invalid/42"

// fakeReporter stands in for observability/sentry, so the chassis's wiring can
// be asserted without linking a vendor SDK into this test.
type fakeReporter struct {
	mu       sync.Mutex
	panics   []any
	shutdown int
	deadline time.Time
	order    *[]string
}

func (f *fakeReporter) CapturePanic(_ context.Context, value any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.panics = append(f.panics, value)
}

func (f *fakeReporter) Shutdown(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shutdown++
	f.deadline, _ = ctx.Deadline()
	if f.order != nil {
		*f.order = append(*f.order, "reporter")
	}
	return nil
}

func (f *fakeReporter) captured() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]any(nil), f.panics...)
}

func (f *fakeReporter) shutdowns() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shutdown
}

// recordingExporter notes when the span pipeline was shut down, so the ORDER of
// the two halves of the telemetry phase is assertable.
type recordingExporter struct {
	mu    sync.Mutex
	order *[]string
}

func (e *recordingExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return nil }

func (e *recordingExporter) Shutdown(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	*e.order = append(*e.order, "otel")
	return nil
}

// TestAPanicReachesTheReporterThroughTheRealStack. The wiring runs from
// Service.Reporter to httpx.Recover's hook and back, through otelhttp, the
// access log and the error writer.
func TestAPanicReachesTheReporterThroughTheRealStack(t *testing.T) {
	t.Parallel()
	rep := &fakeReporter{}
	var gotConfig observability.Config

	c, _ := boot(t, map[string]string{config.KeySentryDSN: testDSN}, chassis.Service{
		Name: "api", Version: "1.4.2", Commit: "abc1234",
		Reporter: func(_ context.Context, cfg observability.Config) (observability.Reporter, error) {
			gotConfig = cfg
			return rep, nil
		},
		Setup: func(context.Context, *chassis.Runtime) (io.Closer, error) { return nil, nil },
	})

	// The chassis, not the process environment, decides the reporter's
	// identity: sentry-go would otherwise read SENTRY_DSN and
	// SENTRY_ENVIRONMENT itself and derive a release from CI variables.
	if gotConfig.DSN != testDSN {
		t.Errorf("DSN = %q", gotConfig.DSN)
	}
	if gotConfig.Service != "api" || gotConfig.Version != "1.4.2" || gotConfig.Commit != "abc1234" {
		t.Errorf("build metadata not passed through: %+v", gotConfig)
	}
	if gotConfig.Env != "development" {
		t.Errorf("Env = %q, want development", gotConfig.Env)
	}

	c.Runtime().Mux.Public.Get("/panic", func(http.ResponseWriter, *http.Request) { panic("boom") })

	srv := &http.Server{Handler: c.Handler()}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	resp, _ := get(t, "http://"+ln.Addr().String()+"/panic")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	got := rep.captured()
	if len(got) != 1 || got[0] != "boom" {
		t.Fatalf("reporter captured %v, want exactly one \"boom\"", got)
	}
}

// TestReporterShutsDownInTheTelemetryPhase, after the span flush.
//
// The phase runs LAST so the drain's own spans still export; the reporter goes
// after OTel within it so a captured issue links to a trace that has already
// been sent.
func TestReporterShutsDownInTheTelemetryPhase(t *testing.T) {
	t.Parallel()
	var order []string
	rep := &fakeReporter{order: &order}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c, err := chassis.New(context.Background(), chassis.Options{
		Service: chassis.Service{
			Name: "api",
			Reporter: func(context.Context, observability.Config) (observability.Reporter, error) {
				return rep, nil
			},
		},
		Lookup: config.MapLookup(map[string]string{
			config.KeySentryDSN:        testDSN,
			config.KeyTelemetryTimeout: "3s",
		}),
		Exporter: &recordingExporter{order: &order},
		Out:      io.Discard,
		Listener: ln,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan lifecycle.Report, 1)
	go func() {
		r, serveErr := c.Serve(ctx)
		if serveErr != nil {
			t.Error(serveErr)
		}
		done <- r
	}()
	<-c.Ready()
	cancel()

	var report lifecycle.Report
	select {
	case report = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("shutdown never finished")
	}

	if _, ok := report.Step(lifecycle.PhaseTelemetry); !ok {
		t.Fatal("no telemetry phase ran")
	}
	if n := rep.shutdowns(); n != 1 {
		t.Fatalf("reporter Shutdown called %d times, want exactly 1", n)
	}
	if len(order) != 2 || order[0] != "otel" || order[1] != "reporter" {
		t.Errorf("telemetry phase order = %v, want [otel reporter]", order)
	}
	// The phase is bounded, and the reporter must be told so: sentry-go's own
	// Close takes no context and can hold the process for ~10s.
	if rep.deadline.IsZero() {
		t.Error("the reporter was given an unbounded context; PhaseTelemetry has a budget")
	}
}

// TestAReporterFailureFailsBoot rather than starting a service that silently
// reports nothing, matching how a bad configuration is treated.
func TestAReporterFailureFailsBoot(t *testing.T) {
	t.Parallel()
	_, err := chassis.New(context.Background(), chassis.Options{
		Service: chassis.Service{
			Name: "api",
			Reporter: func(context.Context, observability.Config) (observability.Reporter, error) {
				return nil, errors.New("the configured DSN was rejected")
			},
		},
		Lookup: config.MapLookup(map[string]string{config.KeySentryDSN: testDSN}),
		Out:    io.Discard,
	})
	if err == nil {
		t.Fatal("a reporter that cannot be built must fail boot")
	}
}

// TestADSNWithNoReporterIsReported. An operator sets SENTRY_DSN, sees no error,
// and gets no issues — the one silent misconfiguration in this wiring.
func TestADSNWithNoReporterIsReported(t *testing.T) {
	t.Parallel()
	_, sink := boot(t, map[string]string{config.KeySentryDSN: testDSN}, chassis.Service{Name: "api"})
	if !sink.Contains("links no error reporter") {
		t.Errorf("no warning for a DSN that reaches nothing:\n%s", sink.String())
	}
}

// TestTheDSNNeverReachesTheBootLine end to end.
//
// Two independent mechanisms hold this: the loader marks the key secret, and
// the logging Guard's denylist contains "dsn". This test therefore proves the
// PROPERTY but cannot tell the two apart — config's own
// TestSentryDSNIsFingerprintedNotEchoed asserts the binding directly, on
// Resolved(), where the Guard cannot mask a regression.
func TestTheDSNNeverReachesTheBootLine(t *testing.T) {
	t.Parallel()
	_, sink := boot(t, map[string]string{config.KeySentryDSN: testDSN}, chassis.Service{Name: "api"})
	if sink.Contains("0123456789abcdef") {
		t.Fatalf("the boot line echoed the DSN:\n%s", sink.String())
	}
	if !sink.Contains(config.KeySentryDSN) {
		t.Errorf("%s is missing from the resolved configuration:\n%s", config.KeySentryDSN, sink.String())
	}
}
