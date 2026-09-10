package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
)

// HandlerFunc is a handler that returns an error.
//
// This signature is what makes safety structural rather than disciplinary: a
// handler has nowhere to render an error itself, so every failure goes through
// ErrorWriter.Fail, which is the only writer of an error body.
type HandlerFunc func(http.ResponseWriter, *http.Request) error

// JSON writes a JSON response.
func JSON(w http.ResponseWriter, status int, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		// Marshalling our own response failed, which is an internal fault and
		// must not be reported as the caller's problem.
		return errs.Internal(err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, werr := w.Write(body)
	return werr
}

// NoContent writes a 204.
func NoContent(w http.ResponseWriter) error {
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Decode reads a JSON body under a byte limit, rejecting unknown fields.
//
// Every failure becomes a typed *errs.Error the client can act on. A raw
// json.SyntaxError must never reach a caller: its offsets describe our own
// schema, so it tells them about our internals rather than about their mistake.
func Decode[T any](r *http.Request, limit int64) (T, error) {
	var zero T

	if ct := r.Header.Get("Content-Type"); ct != "" {
		mt, _, err := mime.ParseMediaType(ct)
		if err != nil || !strings.EqualFold(mt, "application/json") {
			return zero, errs.UnsupportedMediaType([]string{"application/json"})
		}
	}
	if r.Body == nil {
		return zero, errs.InvalidBody(errors.New("empty body"))
	}
	if limit <= 0 {
		limit = 1 << 20
	}

	reader := http.MaxBytesReader(nil, r.Body, limit)
	dec := json.NewDecoder(reader)
	dec.DisallowUnknownFields()

	var out T
	if err := dec.Decode(&out); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return zero, errs.PayloadTooLarge(limit)
		}
		if errors.Is(err, io.EOF) {
			return zero, errs.InvalidBody(errors.New("empty body"))
		}
		// An unknown field is worth naming: it is almost always a typo or a
		// version mismatch, and the caller can fix it.
		if msg := err.Error(); strings.Contains(msg, "unknown field") {
			return zero, errs.Invalid("That request body contains a field this endpoint does not accept.").
				WithFix("Remove it and try again.").
				WithCause(err)
		}
		return zero, errs.InvalidBody(err)
	}
	// A second value means the caller sent a stream, not an object.
	if dec.More() {
		return zero, errs.InvalidBody(errors.New("trailing data after the JSON object"))
	}
	return out, nil
}

// MaxBytes caps a request body for a whole subtree.
func MaxBytes(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if n > 0 && r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}
