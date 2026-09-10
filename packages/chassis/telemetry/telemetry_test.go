package telemetry_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/anasatwork01/cofound/packages/chassis/clock"
	"github.com/anasatwork01/cofound/packages/chassis/logging"
	"github.com/anasatwork01/cofound/packages/chassis/scope"
	"github.com/anasatwork01/cofound/packages/chassis/telemetry"
)

func setup(t *testing.T) (*telemetry.Provider, *tracetest.SpanRecorder) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	p, err := telemetry.Setup(context.Background(), telemetry.Options{
		Config:   telemetry.Config{Service: "api", Version: "1.0.0", Env: "test"},
		Exporter: exporterFor(rec),
		Sync:     true,
		// SetGlobals stays false so these tests run in parallel without
		// fighting over otel's global registry.
	})
	if err != nil {
		t.Fatal(err)
	}
	return p, rec
}

// exporterFor adapts a SpanRecorder, which is a SpanProcessor, to the exporter
// seam. Using WithSyncer on a recorder-backed exporter keeps assertions
// flush-free.
func exporterFor(rec *tracetest.SpanRecorder) sdktrace.SpanExporter {
	return &recorderExporter{rec: rec}
}

type recorderExporter struct{ rec *tracetest.SpanRecorder }

func (e *recorderExporter) ExportSpans(ctx context.Context, ss []sdktrace.ReadOnlySpan) error {
	for _, s := range ss {
		e.rec.OnEnd(s)
	}
	return nil
}
func (e *recorderExporter) Shutdown(context.Context) error { return nil }

// TestBootsWithNoCollector is the property that keeps local development and CI
// usable. OTEL_SDK_DISABLED is not implemented anywhere in the Go SDK, so this
// behaviour has to be ours.
func TestBootsWithNoCollector(t *testing.T) {
	t.Parallel()
	p, err := telemetry.Setup(context.Background(), telemetry.Options{
		Config: telemetry.Config{Service: "api"}, // no endpoint
	})
	if err != nil {
		t.Fatalf("an absent collector must not fail boot: %v", err)
	}
	if p.Enabled() {
		t.Error("Enabled should be false with no exporter")
	}
	// A no-op tracer must still be safe to use everywhere.
	_, span := p.Tracer("t").Start(context.Background(), "op")
	span.End()
	if err := p.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown on a no-op provider: %v", err)
	}
}

// TestConfiguredEndpointWithoutAFactoryIsAnError, rather than silently
// dropping every span. A service that set OTEL_EXPORTER_OTLP_ENDPOINT and got
// no traces would have nothing to debug with.
func TestConfiguredEndpointWithoutAFactoryIsAnError(t *testing.T) {
	t.Parallel()
	_, err := telemetry.Setup(context.Background(), telemetry.Options{
		Config: telemetry.Config{Service: "api", Endpoint: "http://collector:4318"},
	})
	if err == nil {
		t.Fatal("expected an error naming the missing exporter factory")
	}
}

// TestEnrichPutsTheTenancyTupleOnTheSpan is SPEC 17.3's requirement.
func TestEnrichPutsTheTenancyTupleOnTheSpan(t *testing.T) {
	t.Parallel()
	p, rec := setup(t)

	ctx, span := p.Tracer("t").Start(context.Background(), "POST /v1/projects")
	// Enrichment happens after the span starts, because org and project are
	// only known once auth has resolved.
	telemetry.Enrich(ctx, scope.Scope{OrgID: "org_1", ProjectID: "proj_2", SessionID: "sess_3", Turn: 7})
	span.End()
	_ = p.Shutdown(context.Background())

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans", len(spans))
	}
	got := map[string]string{}
	turn := int64(-1)
	for _, a := range spans[0].Attributes() {
		if a.Key == telemetry.KeyTurn {
			turn = a.Value.AsInt64()
			continue
		}
		got[string(a.Key)] = a.Value.AsString()
	}
	for k, want := range map[string]string{
		"halyard.org.id": "org_1", "halyard.project.id": "proj_2", "halyard.session.id": "sess_3",
	} {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
	// Int, not String: a backend should be able to range over a turn number.
	if turn != 7 {
		t.Errorf("halyard.turn = %d, want 7 as an integer attribute", turn)
	}
}

// TestVendorClientDoesNotLeakBaggage is the SPEC 17 boundary test.
//
// otelhttp.NewTransport injects baggage on every request made with that
// client, so one shared traced client would send tenant ids to Stripe, the ad
// platforms and the model providers.
func TestVendorClientDoesNotLeakBaggage(t *testing.T) {
	t.Parallel()
	p, _ := setup(t)

	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	ctx := telemetry.Enrich(context.Background(),
		scope.Scope{OrgID: "org_secret", SessionID: "sess_secret", Turn: 7})

	// Internal hop: baggage is expected and wanted.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if _, err := telemetry.Internal(p.TracerProvider(), nil).Do(req); err != nil {
		t.Fatal(err)
	}
	if got.Get("Baggage") == "" {
		t.Error("an internal hop should carry baggage; that is how sandboxd learns the tenant")
	}

	// Vendor hop: nothing halyard-shaped may cross the perimeter.
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if _, err := telemetry.Vendor(p.TracerProvider(), nil).Do(req2); err != nil {
		t.Fatal(err)
	}
	if b := got.Get("Baggage"); b != "" {
		t.Errorf("vendor client leaked baggage to a third party: %q", b)
	}
	// A trace header is still fine and useful — it identifies no tenant.
	if got.Get("Traceparent") == "" {
		t.Error("vendor client should still propagate traceparent")
	}
}

// TestStripBaggageDefeatsAHandSetHeader: belt and braces, because a caller that
// sets the header itself would bypass the propagator choice.
func TestStripBaggageDefeatsAHandSetHeader(t *testing.T) {
	t.Parallel()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("baggage")
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("baggage", "halyard.org.id=org_1")
	client := &http.Client{Transport: telemetry.StripBaggage(nil)}
	if _, err := client.Do(req); err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("hand-set baggage survived: %q", got)
	}
	// The caller's own request must not be mutated as a side effect.
	if req.Header.Get("baggage") == "" {
		t.Error("StripBaggage mutated the caller's request instead of a clone")
	}
}

// TestScopeFromBaggageIsTelemetryOnly documents, executably, that this value is
// attacker-controlled. The sandbox is untrusted (SPEC 17) and can forge any
// org id here.
func TestScopeFromBaggageReadsWhatTheCallerClaimed(t *testing.T) {
	t.Parallel()
	ctx := telemetry.Enrich(context.Background(), scope.Scope{OrgID: "org_claimed", Turn: 3})
	got := telemetry.ScopeFromBaggage(ctx)
	if got.OrgID != "org_claimed" || got.Turn != 3 {
		t.Errorf("ScopeFromBaggage = %+v", got)
	}
	// Nothing here authenticates the claim, which is why an authorization
	// decision must use auth resolution instead.
}

// TestEnrichOnANonRecordingSpanIsSilent pins a real trap: attributes vanish
// with no error, so a handler enriching from a goroutine after the request
// returned produces spans mysteriously missing every halyard.* attribute.
func TestEnrichOnANonRecordingSpanIsSilent(t *testing.T) {
	t.Parallel()
	p, err := telemetry.Setup(context.Background(), telemetry.Options{
		Config: telemetry.Config{Service: "api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, span := p.Tracer("t").Start(context.Background(), "op")
	span.End()
	// Must not panic, and must not report anything.
	telemetry.Enrich(ctx, scope.Scope{OrgID: "org_1"})
}

// TestErrorHandlerRateLimitsAndReportsTheCount. A dead collector otherwise
// produces a continuous stream of identical lines, which trains people to
// ignore error output.
func TestErrorHandlerRateLimitsAndReportsTheCount(t *testing.T) {
	t.Parallel()
	sink := logging.NewSink()
	log, _ := logging.New(logging.Config{Level: slog.LevelDebug, Format: logging.FormatJSON, Out: sink})
	fake := clock.NewFake(time.Unix(0, 0))
	h := telemetry.NewErrorHandler(log, fake, time.Minute)

	for range 50 {
		h.Handle(errors.New("connection refused"))
	}
	if n := len(sink.Lines()); n != 1 {
		t.Fatalf("emitted %d lines for 50 identical errors, want 1", n)
	}
	if h.Suppressed() != 49 {
		t.Errorf("Suppressed = %d, want 49", h.Suppressed())
	}

	fake.Advance(2 * time.Minute)
	h.Handle(errors.New("connection refused"))
	lines := sink.Lines()
	if len(lines) != 2 {
		t.Fatalf("emitted %d lines after the window, want 2", len(lines))
	}
	if lines[1]["count"] != float64(49) {
		t.Errorf("count = %v, want the suppressed total so a reader can tell a blip from continuous breakage", lines[1]["count"])
	}
	if lines[1]["level"] != "warn" {
		t.Errorf("level = %v, want warn: export failure degrades observability, it does not fail a request", lines[1]["level"])
	}
}

// TestResourceCarriesServiceIdentity, which is how a backend groups spans by
// deployment.
func TestResourceCarriesServiceIdentity(t *testing.T) {
	t.Parallel()
	res, err := telemetry.Resource(telemetry.Config{
		Service: "api", Version: "1.2.3", Env: "production", InstanceID: "i-9", Commit: "abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, a := range res.Attributes() {
		got[string(a.Key)] = a.Value.Emit()
	}
	for k, want := range map[string]string{
		"service.name": "api", "service.version": "1.2.3",
		"service.instance.id": "i-9", "deployment.environment.name": "production",
	} {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
	if res.SchemaURL() != telemetry.SchemaURL {
		t.Errorf("SchemaURL = %q, want %q", res.SchemaURL(), telemetry.SchemaURL)
	}
}
