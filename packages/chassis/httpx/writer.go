// Package httpx is the chassis HTTP layer: middleware, routing, error
// rendering and SSE.
//
// # The Unwrap invariant
//
// EVERY ResponseWriter wrapper in this repository must implement
// Unwrap() http.ResponseWriter.
//
// net/http's ResponseController walks the wrapper chain through an UNEXPORTED
// rwUnwrapper interface (responsecontroller.go), so a middleware that wraps the
// writer without Unwrap silently disables SetWriteDeadline and Flush for every
// SSE stream behind it. There is no compile-time protection: ResponseController
// returns an error rather than failing to build, and the symptom in production
// is a stream truncated at WriteTimeout, which surfaces as a client reconnect
// rather than a server error — so it can hide behind Last-Event-ID resume
// indefinitely while quietly multiplying reconnect load.
//
// Tasks 0.8, 0.10 and 1.14 all add middleware. TestResponseControllerSurvives
// TheProductionStack is the guard.
package httpx

import (
	"io"
	"net/http"
)

// Writer records the status and byte count of a response.
type Writer struct {
	w      http.ResponseWriter
	status int
	bytes  int64
	wrote  bool
}

// Wrap returns a recording writer.
func Wrap(w http.ResponseWriter) *Writer {
	return &Writer{w: w, status: http.StatusOK}
}

// Unwrap exposes the underlying writer.
//
// REQUIRED. See the package doc: without it, ResponseController cannot reach
// the real writer and SSE breaks with no build error.
func (w *Writer) Unwrap() http.ResponseWriter { return w.w }

// Header implements http.ResponseWriter.
func (w *Writer) Header() http.Header { return w.w.Header() }

// WriteHeader implements http.ResponseWriter, recording the first status only.
func (w *Writer) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.status = code
	w.wrote = true
	w.w.WriteHeader(code)
}

// Write implements http.ResponseWriter.
func (w *Writer) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.w.Write(b)
	w.bytes += int64(n)
	return n, err
}

// ReadFrom lets the underlying writer use sendfile where it can.
func (w *Writer) ReadFrom(r io.Reader) (int64, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if rf, ok := w.w.(io.ReaderFrom); ok {
		n, err := rf.ReadFrom(r)
		w.bytes += n
		return n, err
	}
	n, err := io.Copy(w.w, r)
	w.bytes += n
	return n, err
}

// Flush pushes buffered bytes to the client, which is what makes SSE work.
func (w *Writer) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	//nolint:bodyclose // ResponseController, not a response body.
	_ = http.NewResponseController(w.w).Flush()
}

// Status returns the response status.
func (w *Writer) Status() int { return w.status }

// Bytes returns the number of body bytes written.
func (w *Writer) Bytes() int64 { return w.bytes }

// Started reports whether a status has already gone out. After the first byte
// no error envelope can be sent, which is why Recover checks this before
// writing one.
func (w *Writer) Started() bool { return w.wrote }

type writerKey struct{}

// Record installs a recording Writer for the rest of the chain.
func Record(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := Wrap(w)
		next.ServeHTTP(ww, r.WithContext(withWriter(r.Context(), ww)))
	})
}

// WriterFromChain walks the Unwrap chain looking for a *Writer.
func WriterFromChain(w http.ResponseWriter) (*Writer, bool) {
	for {
		if ww, ok := w.(*Writer); ok {
			return ww, true
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return nil, false
		}
		w = u.Unwrap()
	}
}

var (
	_ http.ResponseWriter = (*Writer)(nil)
	_ http.Flusher        = (*Writer)(nil)
	_ io.ReaderFrom       = (*Writer)(nil)
)
