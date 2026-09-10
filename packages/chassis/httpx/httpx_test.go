package httpx_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/chassis/health"
	"github.com/anasatwork01/cofound/packages/chassis/httpx"
	"github.com/anasatwork01/cofound/packages/chassis/lifecycle"
	"github.com/anasatwork01/cofound/packages/chassis/logging"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/common"
)

const dsn = "postgres://halyard:s3cr3t@db.internal:5432/halyard"

type harness struct {
	mux   *httpx.Mux
	sink  *logging.Sink
	spans *tracetest.SpanRecorder
	drain *lifecycle.Drain
	srv   *httptest.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	sink := logging.NewSink()
	log, _ := logging.New(logging.Config{
		Service: "api", Level: slog.LevelDebug, Format: logging.FormatJSON, Out: sink,
	})
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	drain := lifecycle.NewDrain()

	mux := httpx.Router(httpx.RouterConfig{
		Service: "api", Log: log, Errors: httpx.NewErrorWriter(log),
		Drain: drain, TracerProvider: tp,
		HandlerTimeout: 2 * time.Second, MaxBodyBytes: 1 << 16,
		RequestID: httpx.RequestIDConfig{New: func() string { return "REQFIXED01" }},
	})

	h := &harness{mux: mux, sink: sink, spans: rec, drain: drain}
	// BaseContext is where the real server seeds the drain; httptest needs it
	// wired explicitly.
	h.srv = httptest.NewUnstartedServer(mux.Handler())
	h.srv.Config.BaseContext = func(net.Listener) context.Context {
		return httpx.WithDrain(context.Background(), drain)
	}
	t.Cleanup(h.srv.Close)
	return h
}

func (h *harness) start() { h.srv.Start() }

// TestUnexpectedErrorNeverReachesTheClient is THE canary.
//
// A handler returns a raw error carrying a connection string. The credential
// must appear in the log — where it is as privileged as the log itself — and
// must not appear in a single byte of the response.
func TestUnexpectedErrorNeverReachesTheClient(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.mux.Public.Get("/boom", h.errorWriter().H(func(w http.ResponseWriter, r *http.Request) error {
		return fmt.Errorf("query users: dial %s: connection refused", dsn)
	}))
	h.start()

	resp, body := get(t, h.srv.URL+"/boom")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	for _, forbidden := range []string{dsn, "s3cr3t", "db.internal", "connection refused", "query users"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked %q:\n%s", forbidden, body)
		}
	}

	// It must be a decodable envelope, not an empty body.
	var env common.ErrorResponse
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("the generated decoder rejected the envelope: %v\n%s", err, body)
	}
	if env.Error.Code != "internal" {
		t.Errorf("code = %q, want internal", env.Error.Code)
	}
	if env.Error.RequestId == nil || *env.Error.RequestId != "REQFIXED01" {
		t.Error("the envelope must carry the request id, so support can find the log line")
	}

	// The detail must still be diagnosable.
	if !h.sink.Contains("s3cr3t") {
		t.Fatalf("the cause never reached the log, making the fault undiagnosable:\n%s", h.sink.String())
	}
}

// TestRecoverPositionIsObservable pins the counterintuitive middleware order.
//
// With recovery OUTSIDE tracing and logging the client still gets a 500, so
// nothing looks broken — but the access log records status 0 and the span stays
// Unset. This test is what stops someone "fixing" the order.
func TestRecoverPositionIsObservable(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.mux.Public.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})
	h.start()

	resp, body := get(t, h.srv.URL+"/panic")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	// chi's Recoverer answers a panic with an EMPTY body, which every
	// generated client decoder fails to parse.
	var env common.ErrorResponse
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("a panic must still produce a decodable envelope: %v\n%q", err, body)
	}

	// The access log must record 500, not 0.
	var access map[string]any
	for _, line := range h.sink.Lines() {
		if line["msg"] == "http request" {
			access = line
		}
	}
	if access == nil {
		t.Fatal("no access log line was emitted for a panicking request")
	}
	if access["http_status"] != float64(500) {
		t.Errorf("access log http_status = %v, want 500 — recovery is outside logging", access["http_status"])
	}

	// The span must record the error.
	spans := h.spans.Ended()
	if len(spans) == 0 {
		t.Fatal("no span was recorded")
	}
	last := spans[len(spans)-1]
	if last.Status().Code != codes.Error {
		t.Errorf("span status = %v, want Error — recovery is outside tracing", last.Status().Code)
	}
}

// TestPanicAndReturnedErrorAreIndistinguishable, so a client never has to
// handle two shapes of the same failure.
func TestPanicAndReturnedErrorAreIndistinguishable(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ew := h.errorWriter()
	h.mux.Public.Get("/panic", func(w http.ResponseWriter, r *http.Request) { panic("boom") })
	h.mux.Public.Get("/return", ew.H(func(w http.ResponseWriter, r *http.Request) error {
		return errors.New("boom")
	}))
	h.start()

	_, panicBody := get(t, h.srv.URL+"/panic")
	_, returnBody := get(t, h.srv.URL+"/return")
	if panicBody != returnBody {
		t.Errorf("bodies differ:\n panic: %s\nreturn: %s", panicBody, returnBody)
	}
}

// TestChiDefaultsAreReplaced. chi answers an unknown route with text/plain and
// a 405 with plain text; both break every generated client decoder.
func TestChiDefaultsAreReplaced(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.mux.Public.Get("/only-get", func(w http.ResponseWriter, r *http.Request) {})
	h.start()

	for _, tc := range []struct {
		name, method, path string
		want               int
		wantCode           string
	}{
		{"unknown route", http.MethodGet, "/nope", 404, "not_found"},
		{"wrong method", http.MethodPost, "/only-get", 405, "method_not_allowed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(tc.method, h.srv.URL+tc.path, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.want)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("content-type = %q, want JSON", ct)
			}
			var env common.ErrorResponse
			if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
				t.Fatalf("not a decodable envelope: %v", err)
			}
			if env.Error.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", env.Error.Code, tc.wantCode)
			}
		})
	}
}

// TestRequestIDIsNeverTakenFromTheClientByDefault. chi's middleware copies
// X-Request-Id verbatim, handing an attacker the value that lands in the
// envelope and every log line.
func TestRequestIDIsNeverTakenFromTheClientByDefault(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.mux.Public.Get("/ok", func(w http.ResponseWriter, r *http.Request) {})
	h.start()

	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/ok", nil)
	req.Header.Set(httpx.HeaderRequestID, "injected;level=error")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get(httpx.HeaderRequestID); got != "REQFIXED01" {
		t.Errorf("request id = %q, want the generated one", got)
	}
	if h.sink.Contains("injected;level=error") {
		t.Errorf("a client-supplied id reached the log:\n%s", h.sink.String())
	}
}

func TestValidRequestIDRejectsInjection(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"", "short", "has space", "has\nnewline", strings.Repeat("a", 65), "semi;colon"} {
		if httpx.ValidRequestID(bad) {
			t.Errorf("%q must be rejected", bad)
		}
	}
	for _, ok := range []string{"ABCDEFGH", "a-b_c-1234", strings.Repeat("a", 64)} {
		if !httpx.ValidRequestID(ok) {
			t.Errorf("%q must be accepted", ok)
		}
	}
}

// TestResponseControllerSurvivesTheProductionStack is the SSE guard.
//
// SetWriteDeadline reaches the real writer only if every wrapper in the chain
// implements Unwrap. otelhttp, Record and chi are all in this path. If a future
// middleware forgets Unwrap, streams truncate at WriteTimeout with no build
// error and no runtime warning — so the assertion has to run through the real
// assembled stack, not a bare handler.
func TestResponseControllerSurvivesTheProductionStack(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	var cleared bool
	h.mux.Stream.Get("/events", h.errorWriter().H(func(w http.ResponseWriter, r *http.Request) error {
		s, err := httpx.Open(w, r, httpx.StreamOptions{Kind: "session_events"})
		if err != nil {
			return err
		}
		defer s.Close(httpx.ClosedDone)
		cleared = s.DeadlineCleared()
		return s.Send(httpx.Event{ID: "1", Name: "tick", Data: []byte(`{"n":1}`)})
	}))
	// A short WriteTimeout is what a truncation bug would trip over.
	h.srv.Config.WriteTimeout = 200 * time.Millisecond
	h.start()

	resp, body := get(t, h.srv.URL+"/events")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !cleared {
		t.Fatal("SetWriteDeadline did not reach the real ResponseWriter: a middleware in the chain is missing Unwrap() http.ResponseWriter")
	}
	if !strings.Contains(body, "event: tick") {
		t.Errorf("frame missing:\n%q", body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("content-type = %q", ct)
	}
}

// TestStreamRefusedOnceDraining. Starting a stream mid-shutdown would cut it
// immediately; a 503 lets the client reconnect to a healthy replica.
func TestStreamRefusedOnceDraining(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.mux.Stream.Get("/events", h.errorWriter().H(func(w http.ResponseWriter, r *http.Request) error {
		s, err := httpx.Open(w, r, httpx.StreamOptions{Kind: "session_events"})
		if err != nil {
			return err
		}
		defer s.Close(httpx.ClosedDone)
		return nil
	}))
	h.start()
	h.drain.Begin()

	resp, body := get(t, h.srv.URL+"/events")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("a refused stream must say when to come back")
	}
	var env common.ErrorResponse
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("not an envelope: %v\n%s", err, body)
	}
}

// TestStreamContextIsCancelledByTheDrain proves a handler's entire shutdown
// participation is using s.Context().
func TestStreamContextIsCancelledByTheDrain(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	opened := make(chan struct{})
	closedCleanly := make(chan bool, 1)

	h.mux.Stream.Get("/events", h.errorWriter().H(func(w http.ResponseWriter, r *http.Request) error {
		s, err := httpx.Open(w, r, httpx.StreamOptions{Kind: "session_events"})
		if err != nil {
			return err
		}
		close(opened)
		<-s.Context().Done()
		closedCleanly <- s.IsDraining()
		return s.Close(httpx.ClosedDraining)
	}))
	h.start()

	go func() { _, _ = get(t, h.srv.URL+"/events") }()
	<-opened
	if got := h.drain.Open()["session_events"]; got != 1 {
		t.Fatalf("drain did not register the stream: %v", h.drain.Open())
	}
	h.drain.Begin()

	select {
	case draining := <-closedCleanly:
		if !draining {
			t.Error("the handler could not tell a drain from a disconnect")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the drain broadcast never reached the stream context")
	}
}

// TestEventEncodeFoldsMultilineData. A raw newline in data would make the
// client reassemble the payload wrongly, and a newline in id or event would let
// a caller inject extra frames.
func TestEventEncodeFoldsMultilineData(t *testing.T) {
	t.Parallel()
	got := string(httpx.Event{ID: "7", Name: "message.delta", Data: []byte("line one\r\nline two\rline three")}.Encode())
	want := "id: 7\nevent: message.delta\ndata: line one\ndata: line two\ndata: line three\n\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}

	// A newline in id or event must not be able to start a new directive line.
	// The literal text surviving inside the id VALUE is harmless; a line
	// beginning "event:" that we did not author is not.
	injected := string(httpx.Event{ID: "1\nevent: forged", Data: []byte("x")}.Encode())
	for _, line := range strings.Split(injected, "\n") {
		if strings.HasPrefix(line, "event:") {
			t.Errorf("frame injection produced a directive line %q in:\n%q", line, injected)
		}
	}
}

// TestDecodeTurnsParserFailuresIntoActionableErrors. A json.SyntaxError's
// offsets describe our schema, not the caller's mistake.
func TestDecodeTurnsParserFailuresIntoActionableErrors(t *testing.T) {
	t.Parallel()
	type body struct {
		Name string `json:"name"`
	}
	cases := []struct {
		name, ct, payload string
		wantCode          errs.Code
	}{
		{"malformed", "application/json", `{"name":`, errs.CodeInvalid},
		{"unknown field", "application/json", `{"nome":"x"}`, errs.CodeInvalid},
		{"empty", "application/json", ``, errs.CodeInvalid},
		{"wrong type", "text/plain", `{"name":"x"}`, errs.CodeUnsupportedMediaType},
		{"trailing data", "application/json", `{"name":"x"}{"name":"y"}`, errs.CodeInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.payload))
			r.Header.Set("Content-Type", tc.ct)
			_, err := httpx.Decode[body](r, 1<<16)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !errors.Is(err, tc.wantCode) {
				t.Errorf("code = %s, want %s", errs.CodeOf(err), tc.wantCode)
			}
			// The parser's own complaint must not reach the client message.
			var e *errs.Error
			errors.As(err, &e)
			if strings.Contains(e.Message, "offset") || strings.Contains(e.Message, "json:") {
				t.Errorf("parser detail leaked into the client message: %q", e.Message)
			}
		})
	}
}

func TestDecodeRejectsAnOversizedBody(t *testing.T) {
	t.Parallel()
	type body struct {
		Name string `json:"name"`
	}
	big := `{"name":"` + strings.Repeat("a", 5000) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(big))
	r.Header.Set("Content-Type", "application/json")
	_, err := httpx.Decode[body](r, 100)
	if !errors.Is(err, errs.CodePayloadTooLarge) {
		t.Errorf("code = %s, want payload_too_large", errs.CodeOf(err))
	}
}

// TestHealthzIsAliveWithoutDependencies. Liveness must not depend on a
// database, or one blip makes the orchestrator restart every replica.
func TestHealthzIsAliveWithoutDependencies(t *testing.T) {
	t.Parallel()
	sink := logging.NewSink()
	log, _ := logging.New(logging.Config{Level: slog.LevelDebug, Format: logging.FormatJSON, Out: sink})
	reg := health.New(health.Options{})
	reg.Register(health.PingFunc("postgres", func(context.Context) error {
		return errors.New("down")
	}, health.Critical))

	mux := httpx.Router(httpx.RouterConfig{
		Service: "api", Version: "1.0.0", Log: log, Health: reg,
		Drain: lifecycle.NewDrain(), HandlerTimeout: time.Second,
	})
	srv := httptest.NewServer(mux.Handler())
	defer srv.Close()

	resp, body := get(t, srv.URL+"/healthz")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz = %d, want 200 even with a dependency down", resp.StatusCode)
	}
	if !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("body = %s", body)
	}

	// Readiness, by contrast, must reflect it.
	rresp, rbody := get(t, srv.URL+"/readyz")
	if rresp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("readyz = %d, want 503", rresp.StatusCode)
	}
	if !strings.Contains(rbody, "postgres") {
		t.Errorf("readyz should name the failing probe: %s", rbody)
	}
	// But never its error string: /readyz is unauthenticated.
	if strings.Contains(rbody, "down") && strings.Contains(rbody, "error\":\"down") {
		t.Errorf("probe error string leaked to an unauthenticated endpoint: %s", rbody)
	}
}

func (h *harness) errorWriter() *httpx.ErrorWriter {
	log, _ := logging.New(logging.Config{Level: slog.LevelDebug, Format: logging.FormatJSON, Out: h.sink})
	return httpx.NewErrorWriter(log)
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
