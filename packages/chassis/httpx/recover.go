package httpx

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/chassis/logkey"
)

// Recover turns a panic into the same envelope a returned error produces.
//
// It sits INSIDE both tracing and logging, which is counterintuitive and
// measured: that is the only position where the access log records 500 and the
// span records status=Error. With recovery outermost the client still gets a
// 500 so nothing looks broken, while the log records status 0 and the span
// stays Unset — panic blindness in exactly the incident that needs the trace.
//
// It does three things chi's Recoverer does not do together: re-panics
// http.ErrAbortHandler, checks Started() before writing a status, and delegates
// rendering to ErrorWriter.Fail so a panic and a returned error produce
// byte-identical bodies. chi's Recoverer answers a panic with an EMPTY body,
// which every generated client decoder fails to parse.
//
// hook is the error-reporter seam and may be nil. It is a hook rather than a
// second middleware on purpose: sentry-go's own HTTP middleware has Repanic
// false by default, so stacking it would swallow the panic before it ever
// reached this recoverer and the client would get sentry's empty body instead
// of the envelope. One recoverer, one envelope.
func Recover(ew *ErrorWriter, hook PanicHook) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				// ErrAbortHandler is net/http's documented way to abandon a
				// response; swallowing it would break that contract.
				if rec == http.ErrAbortHandler {
					panic(rec)
				}

				stack := string(debug.Stack())
				ew.Log.LogAttrs(r.Context(), slog.LevelError, "handler panicked",
					slog.Any(logkey.PanicValue, fmt.Sprint(rec)),
					slog.String(logkey.Stack, stack),
				)

				// AFTER the ErrAbortHandler re-panic, so a deliberate abort is
				// never filed as an issue, and BEFORE Fail, so the capture
				// happens on the still-unwinding stack: the panicking frames
				// are live here and gone once this defer returns, and a
				// reporter that symbolises its own stack needs them.
				//
				// Positioning it before Fail also means the capture cannot be
				// skipped by the mid-stream early return below.
				if hook != nil {
					hook(r.Context(), rec)
				}

				// A panic mid-stream cannot be rendered: chi's unconditional
				// WriteHeader(500) there hands the client a truncated 200 with
				// no error signal, plus a superfluous-WriteHeader warning.
				if ww, ok := WriterFromChain(w); ok && ww.Started() {
					return
				}
				// Never the stack in a response body.
				ew.Fail(w, r, errs.Internal(fmt.Errorf("panic: %v", rec)))
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// PanicHook reports a recovered panic to an error reporter. nil means none.
//
// It receives the request context, which carries the active OpenTelemetry span,
// so a captured issue and its trace can find each other. It is NOT given the
// formatted stack: it runs on the panicking goroutine while the frames are
// still live, which is strictly more than a string.
//
// It runs on the request's own goroutine during the unwind. It must not panic
// and must not block.
type PanicHook func(ctx context.Context, value any)
