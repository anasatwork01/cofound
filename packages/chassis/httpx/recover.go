package httpx

import (
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
func Recover(ew *ErrorWriter) func(http.Handler) http.Handler {
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
