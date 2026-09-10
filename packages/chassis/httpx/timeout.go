package httpx

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
)

// Timeout bounds a handler with a context deadline.
//
// It calls the handler INLINE and does not spawn a goroutine. That is a
// correctness requirement, not a style choice: Recover sits outside Timeout in
// the stack, and a deferred recover() only catches panics on its own goroutine.
// Running the handler in a goroutine therefore let a panicking handler escape
// recovery entirely and crash the process — caught by
// TestRecoverPositionIsObservable. Spawning also allows the handler and the
// timeout path to write to the same ResponseWriter concurrently.
//
// The consequence, stated plainly: this bounds handlers that respect their
// context, which is the honest meaning of "context-based". A handler that
// blocks in a syscall ignoring ctx is bounded by the server's own timeouts, not
// by this.
//
// Applied PER SUBTREE and never to a streaming group. A context timeout kills
// SSE just as surely as a write deadline does — measured: 3 of 6 events
// delivered at a 500ms handler timeout — so excluding streams via Mux.Stream is
// the mechanism, not tuning the value up.
//
// Never http.TimeoutHandler: it does not implement http.Flusher, so Flush
// becomes "feature not supported" for every stream behind it. Banned repo-wide.
func Timeout(d time.Duration, ew *ErrorWriter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if d <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()

			next.ServeHTTP(w, r.WithContext(ctx))

			// The handler returned. If it overran and wrote nothing, say so
			// rather than leaving the client with an empty 200.
			if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return
			}
			if ww, ok := WriterFromChain(w); ok && ww.Started() {
				return
			}
			ew.Fail(w, r, errs.Timeout("that request", ctx.Err()))
		})
	}
}
