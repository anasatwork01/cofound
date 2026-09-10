package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/chassis/scope"
	"github.com/anasatwork01/cofound/packages/chassis/telemetry"
)

// ErrorWriter renders errors. Injected rather than global, so a test can assert
// against its own logger and two tests can run in parallel.
type ErrorWriter struct{ Log *slog.Logger }

// NewErrorWriter returns a writer logging to log.
func NewErrorWriter(log *slog.Logger) *ErrorWriter {
	if log == nil {
		log = slog.Default()
	}
	return &ErrorWriter{Log: log}
}

// Fail is the ONLY place an error response body is written.
//
// The ordering is the safety property:
//  1. errs.From maps anything that is not a typed error to a canned internal
//     fault, so an unexpected error cannot select its own message.
//  2. The full chain — including the cause — goes to the log and the span.
//  3. The wire body is built by errs.Wire from authored strings only. There is
//     no code path from a cause to a response field, so a returned pgx error
//     structurally cannot put a connection string on the wire.
func (ew *ErrorWriter) Fail(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	ctx := r.Context()
	e := errs.From(err)
	requestID := scope.RequestID(ctx)

	logError(ctx, ew.Log, e, requestID)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(telemetry.KeyErrorCode.String(string(e.Code)))
	if e.Status >= 500 {
		// RecordError puts the cause on the span, where it is as privileged as
		// the log — and only there.
		span.RecordError(e, trace.WithStackTrace(true))
		span.SetStatus(codes.Error, string(e.Code))
	}

	// The client is gone. Writing would be pointless and would log a
	// superfluous-WriteHeader warning.
	if e.Code == errs.CodeClientClosed {
		return
	}

	// Something has already been written — mid-stream, or after a partial
	// response. A status cannot be changed now, and writing an envelope would
	// corrupt the body the client is already parsing.
	if ww, ok := WriterFromChain(w); ok && ww.Started() {
		return
	}

	if e.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(e.RetryAfter.Seconds())))
	}
	body, mErr := json.Marshal(e.Response(requestID))
	if mErr != nil {
		// Last resort: a hand-rolled envelope, so a client still gets
		// something its decoder can parse.
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal","message":"Halyard could not complete this request.","retriable":false}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(e.Status)
	_, _ = w.Write(body)
}

// H adapts a HandlerFunc to net/http.
func (ew *ErrorWriter) H(h HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			ew.Fail(w, r, err)
		}
	}
}

// NotFound replaces chi's default, which writes text/plain that every generated
// client decoder fails to parse.
func (ew *ErrorWriter) NotFound() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ew.Fail(w, r, errs.RouteNotFound())
	}
}

// MethodNotAllowed replaces chi's default plain-text 405.
func (ew *ErrorWriter) MethodNotAllowed() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ew.Fail(w, r, errs.MethodNotAllowed(r.Method, nil))
	}
}
