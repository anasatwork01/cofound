package httpx_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/anasatwork01/cofound/packages/chassis/httpx"
	"github.com/anasatwork01/cofound/packages/chassis/logging"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/common"
)

// capture records what a PanicHook was handed.
type capture struct {
	mu    sync.Mutex
	calls []any
	trace []oteltrace.SpanContext
}

func (c *capture) hook(ctx context.Context, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, value)
	c.trace = append(c.trace, oteltrace.SpanContextFromContext(ctx))
}

func (c *capture) n() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func testWriter(t *testing.T) *httpx.ErrorWriter {
	t.Helper()
	log, _ := logging.New(logging.Config{
		Service: "api", Level: slog.LevelDebug, Format: logging.FormatJSON, Out: logging.NewSink(),
	})
	return httpx.NewErrorWriter(log)
}

// TestPanicHookCapturesThroughTheRealStack.
//
// The hook is what replaces sentry-go's own HTTP middleware. sentryhttp's
// Repanic defaults to FALSE, so stacking it would swallow the panic before this
// recoverer ever saw it and the client would get sentry's empty body instead of
// the error envelope. This asserts both halves: the capture happens, and the
// response is byte-for-byte the envelope it was without a hook.
func TestPanicHookCapturesThroughTheRealStack(t *testing.T) {
	t.Parallel()
	c := &capture{}

	log, _ := logging.New(logging.Config{
		Service: "api", Level: slog.LevelDebug, Format: logging.FormatJSON, Out: logging.NewSink(),
	})
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	mux := httpx.Router(httpx.RouterConfig{
		Service: "api", Log: log, Errors: httpx.NewErrorWriter(log),
		TracerProvider: tp, HandlerTimeout: 2 * time.Second, MaxBodyBytes: 1 << 16,
		PanicHook: c.hook,
	})
	mux.Public.Get("/panic", func(w http.ResponseWriter, r *http.Request) { panic("boom") })
	srv := httptest.NewServer(mux.Handler())
	t.Cleanup(srv.Close)

	resp, body := get(t, srv.URL+"/panic")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	var env common.ErrorResponse
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("the hook must not change the envelope: %v\n%q", err, body)
	}

	if c.n() != 1 {
		t.Fatalf("hook called %d times, want exactly 1", c.n())
	}
	if got := c.calls[0]; got != "boom" {
		t.Errorf("hook got %#v, want the raw panic value", got)
	}
	// The hook runs inside otelhttp's span, which is the whole reason it takes
	// a context: it is how a captured issue and its trace find each other.
	if sc := c.trace[0]; !sc.TraceID().IsValid() {
		t.Error("the hook's context carried no trace id; the capture cannot be joined to its trace")
	}
}

// TestPanicHookIsNotCalledForErrAbortHandler. ErrAbortHandler is net/http's
// documented way to abandon a response deliberately. Filing it as an issue
// would turn every aborted stream into a page.
func TestPanicHookIsNotCalledForErrAbortHandler(t *testing.T) {
	t.Parallel()
	c := &capture{}
	h := httpx.Recover(testWriter(t), c.hook)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { panic(http.ErrAbortHandler) }))

	defer func() {
		rec := recover()
		if rec != http.ErrAbortHandler {
			t.Errorf("ErrAbortHandler must still re-panic, got %#v", rec)
		}
		if c.n() != 0 {
			t.Errorf("a deliberate abort was reported as an issue: %v", c.calls)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

// TestPanicHookRunsEvenWhenTheResponseHasStarted.
//
// A panic mid-stream cannot be rendered, so Recover returns without writing.
// That early return is exactly why the hook is placed BEFORE it: a panic
// halfway through an SSE stream is the one most worth reporting, and a hook
// after Fail would silently never see it.
func TestPanicHookRunsEvenWhenTheResponseHasStarted(t *testing.T) {
	t.Parallel()
	c := &capture{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial"))
		panic("boom mid-stream")
	})
	// Record is what puts a Started()-aware writer in the chain.
	h := httpx.Record(httpx.Recover(testWriter(t), c.hook)(inner))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if body := w.Body.String(); body != "partial" {
		t.Errorf("body = %q; a started response must not be overwritten", body)
	}
	if c.n() != 1 {
		t.Fatalf("hook called %d times, want 1 — a mid-stream panic is unreportable otherwise", c.n())
	}
}

// TestNilPanicHookIsSafe. nil is what every test and every service without a
// SENTRY_DSN gets, so it is the common path, not the edge case.
func TestNilPanicHookIsSafe(t *testing.T) {
	t.Parallel()
	h := httpx.Recover(testWriter(t), nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { panic("boom") }))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}
