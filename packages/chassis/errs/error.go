package errs

import (
	"errors"
	"fmt"
	"maps"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Error is the chassis error.
//
// Message is authored and client-safe. It is never derived from cause: cause
// reaches Error(), which reaches slog and span.RecordError, and nothing else.
type Error struct {
	Code       Code
	Status     int
	Message    string
	Fix        string
	Retriable  bool
	RetryAfter time.Duration
	Details    map[string]any

	cause error
	stack []uintptr
}

// Error implements error. This string is for the log and the span. It is never
// a response body.
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(string(e.Code))
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	if e.cause != nil {
		b.WriteString(": ")
		b.WriteString(e.cause.Error())
	}
	return b.String()
}

// Unwrap exposes the cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.cause }

// Is matches against a Code, so errors.Is(err, errs.CodeNotFound) works.
func (e *Error) Is(target error) bool {
	if c, ok := target.(Code); ok {
		return e.Code == c
	}
	return false
}

// Cause returns the wrapped error, if any.
func (e *Error) Cause() error { return e.cause }

// Stack returns the resolved frames captured for a 5xx. Log only — a stack
// trace in a response body is an information leak.
func (e *Error) Stack() string {
	if len(e.stack) == 0 {
		return ""
	}
	var b strings.Builder
	frames := runtime.CallersFrames(e.stack)
	for {
		f, more := frames.Next()
		if f.Function != "" {
			b.WriteString(f.Function)
			b.WriteString("\n\t")
			b.WriteString(f.File)
			b.WriteString(":")
			b.WriteString(strconv.Itoa(f.Line))
			b.WriteString("\n")
		}
		if !more {
			break
		}
	}
	return b.String()
}

// clone copies e so a builder never mutates a shared value. Catalogue-derived
// errors are handed out repeatedly; without this, two goroutines calling
// WithDetail on the same base would race and cross-contaminate.
func (e *Error) clone() *Error {
	out := *e
	if e.Details != nil {
		out.Details = maps.Clone(e.Details)
	}
	return &out
}

// WithCause attaches a cause and, for a 5xx, captures a stack.
func (e *Error) WithCause(err error) *Error {
	out := e.clone()
	out.cause = err
	if out.Status >= 500 && out.stack == nil {
		out.stack = capture()
	}
	return out
}

// WithFix replaces the remedial half of the message (SPEC 18).
func (e *Error) WithFix(fix string) *Error {
	out := e.clone()
	out.Fix = fix
	return out
}

// WithMessage replaces the authored, client-safe message.
func (e *Error) WithMessage(msg string) *Error {
	out := e.clone()
	out.Message = msg
	return out
}

// WithDetail adds one detail. Never a secret value (SPEC 17.2).
func (e *Error) WithDetail(k string, v any) *Error {
	out := e.clone()
	if out.Details == nil {
		out.Details = map[string]any{}
	}
	out.Details[k] = v
	return out
}

// WithDetails merges details.
func (e *Error) WithDetails(d map[string]any) *Error {
	out := e.clone()
	if len(d) == 0 {
		return out
	}
	if out.Details == nil {
		out.Details = make(map[string]any, len(d))
	}
	maps.Copy(out.Details, d)
	return out
}

// WithRetryAfter sets the Retry-After header value.
func (e *Error) WithRetryAfter(d time.Duration) *Error {
	out := e.clone()
	out.RetryAfter = d
	return out
}

// WithStatus overrides the HTTP status.
func (e *Error) WithStatus(status int) *Error {
	out := e.clone()
	out.Status = status
	return out
}

func capture() []uintptr {
	pc := make([]uintptr, 32)
	// Skip runtime.Callers, capture and the With* builder that called it.
	n := runtime.Callers(3, pc)
	return pc[:n]
}

// StatusOf returns the HTTP status for err, defaulting to 500.
func StatusOf(err error) int {
	var e *Error
	if errors.As(err, &e) && e.Status != 0 {
		return e.Status
	}
	return 500
}

// CodeOf returns the code for err, defaulting to CodeInternal.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) && e.Code != "" {
		return e.Code
	}
	return CodeInternal
}

var _ error = (*Error)(nil)
var _ fmt.Stringer = Code("")

// String implements fmt.Stringer for Code.
func (c Code) String() string { return string(c) }
