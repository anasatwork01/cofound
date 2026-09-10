package errs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// Named constructors carry their own authored literals, so rendering needs no
// catalogue lookup and Wire stays a pure function of the error.

// Invalid reports a request that was understood but is not valid.
func Invalid(msg string) *Error {
	e := Chassis().New(CodeInvalid)
	return e.WithMessage(msg)
}

// InvalidField names the offending field and the shape wanted.
func InvalidField(field, want string) *Error {
	return Chassis().New(CodeInvalid).
		WithMessage(fmt.Sprintf("%s %s.", field, want)).
		WithFix("Correct it and try again.").
		WithDetail("field", field)
}

// InvalidBody reports an unparseable body.
//
// The message is generic on purpose: a json.SyntaxError's offsets describe our
// own schema, and echoing the parser's complaint tells a caller more about our
// internals than about their mistake. The detail goes to the log.
func InvalidBody(cause error) *Error {
	return Chassis().New(CodeInvalid).
		WithMessage("That request body could not be read as JSON.").
		WithFix("Send a valid JSON object.").
		WithCause(cause)
}

// Unauthenticated reports a missing or invalid session.
func Unauthenticated() *Error { return Chassis().New(CodeUnauthenticated) }

// Forbidden reports a role that may not perform action.
//
// A permission check that fails because of TENANCY is a NotFound, never a
// Forbidden: api.openapi.yaml makes cross-tenant reads indistinguishable from
// absence on purpose, and a 403 confirms the resource exists.
func Forbidden(action string) *Error {
	e := Chassis().New(CodeForbidden)
	if action == "" {
		return e
	}
	return e.WithMessage(fmt.Sprintf("Your role does not allow you to %s.", action))
}

// NotFound reports an absent — or invisible — resource.
//
// Details is fixed to {"resource": kind}. There is deliberately no way to
// attach a reason, an owner, an org id or an "exists elsewhere" hint: any of
// those turns this into an existence oracle across tenants. ident is
// sanitised, and dropped entirely if it does not survive.
func NotFound(kind, ident string) *Error {
	e := Chassis().New(CodeNotFound)
	msg := "That does not exist."
	if kind != "" {
		msg = fmt.Sprintf("That %s does not exist.", kind)
		e = e.WithDetail("resource", kind)
	}
	if clean, ok := SanitizeIdent(ident); ok {
		msg = fmt.Sprintf("%s %q does not exist.", titleFirst(kind), clean)
	}
	return e.WithMessage(msg)
}

func titleFirst(s string) string {
	if s == "" {
		return "That"
	}
	r, size := utf8.DecodeRuneInString(s)
	return strings.ToUpper(string(r)) + s[size:]
}

// NotFoundOnNoRows converts a no-rows sentinel into a 404 and leaves anything
// else alone, so a handler's happy path stays free of error mapping.
func NotFoundOnNoRows(err error, kind, ident string) *Error {
	if err == nil {
		return nil
	}
	if isNoRows(err) {
		return NotFound(kind, ident).WithCause(err)
	}
	return nil
}

// isNoRows matches by string because the chassis must not depend on a database
// driver, and that stays true now that task 0.6 has added pgx: giving the
// chassis a pgx dependency would make every service that never touches Postgres
// -- and the chassis's own tests -- build it.
//
// packages/db.NotFoundOnNoRows is the precise version, using
// errors.Is(err, pgx.ErrNoRows). A service that already imports that package
// should prefer it; this remains for callers that do not.
func isNoRows(err error) bool {
	s := err.Error()
	return strings.Contains(s, "no rows in result set") || strings.Contains(s, "sql: no rows")
}

// RouteNotFound replaces chi's http.NotFound, which writes text/plain that
// every generated client decoder fails to parse.
func RouteNotFound() *Error {
	return Chassis().New(CodeNotFound).
		WithMessage("There is no endpoint at that path.").
		WithFix("Check the path and the API version.")
}

// MethodNotAllowed reports a wrong method and advertises the right ones.
func MethodNotAllowed(method string, allowed []string) *Error {
	e := Chassis().New(CodeMethodNotAllowed)
	if method != "" {
		e = e.WithMessage(fmt.Sprintf("%s is not allowed on that path.", strings.ToUpper(method)))
	}
	if len(allowed) > 0 {
		e = e.WithFix("Use "+strings.Join(allowed, ", ")+".").
			WithDetail("allowed", allowed)
	}
	return e
}

// Conflict reports a state conflict under a specific code, so a service can
// distinguish its own 409s.
func Conflict(c Code, msg string) *Error {
	e := Chassis().New(CodeConflict)
	if c != "" && c.Valid() {
		e.Code = c
	}
	if msg != "" {
		e = e.WithMessage(msg)
	}
	return e
}

// PaymentRequired reports insufficient credits.
//
// SPEC 16.3: exhausting the build meter pauses the builder, which is harmless
// because the user is at the keyboard.
func PaymentRequired(needed, available string) *Error {
	return Chassis().New(CodePaymentRequired).
		WithFix("Top up credits, or turn on auto top-up.").
		WithDetails(map[string]any{"credits_needed": needed, "credits_available": available})
}

// PayloadTooLarge reports a body over the limit.
func PayloadTooLarge(limit int64) *Error {
	return Chassis().New(CodePayloadTooLarge).
		WithFix(fmt.Sprintf("Send at most %d bytes.", limit)).
		WithDetail("limit_bytes", limit)
}

// UnsupportedMediaType reports a content type we do not accept.
func UnsupportedMediaType(want []string) *Error {
	e := Chassis().New(CodeUnsupportedMediaType)
	if len(want) > 0 {
		e = e.WithFix("Send " + strings.Join(want, " or ") + ".")
	}
	return e
}

// RateLimited reports throttling and sets Retry-After.
func RateLimited(retryAfter time.Duration) *Error {
	return Chassis().New(CodeRateLimited).WithRetryAfter(retryAfter)
}

// Unavailable reports a dependency being down.
//
// display is USER-FACING: "the model provider", never a hostname. A hostname in
// a client-visible message is internal topology, and it is the kind of detail
// that ends up in a screenshot on a public forum.
func Unavailable(display string, cause error) *Error {
	e := Chassis().New(CodeUnavailable)
	if display != "" {
		e = e.WithMessage(fmt.Sprintf("%s is unavailable right now.", titleFirst(display)))
	}
	return e.WithCause(cause)
}

// Timeout reports a dependency exceeding its budget.
func Timeout(display string, cause error) *Error {
	e := Chassis().New(CodeTimeout)
	if display != "" {
		e = e.WithMessage(fmt.Sprintf("%s did not respond in time.", titleFirst(display)))
	}
	return e.WithCause(cause)
}

// Internal wraps an unexpected fault. The cause never reaches the client.
func Internal(cause error) *Error {
	return Chassis().New(CodeInternal).WithCause(cause)
}

// NotReady reports failing readiness probes by name only — never their error
// strings, because /readyz is unauthenticated.
func NotReady(failed []string) *Error {
	e := Chassis().New(CodeNotReady)
	if len(failed) > 0 {
		e = e.WithDetail("failing", failed)
	}
	return e
}

// ClientClosed marks a disconnect. Never rendered: there is nobody to render
// for. It exists so the access log can tell a disconnect from a fault, and so
// a disconnect is not counted as a 5xx against the availability SLO.
func ClientClosed(cause error) *Error {
	return &Error{
		Code:    CodeClientClosed,
		Status:  499,
		Message: "The client closed the connection.",
		cause:   cause,
	}
}

// SanitizeIdent guards the one caller-supplied string reflected into a message.
//
// Without it a caller controls text that lands in a JSON body and in log lines:
// unbounded length, newlines for log forging, and anything a console renders.
func SanitizeIdent(s string) (string, bool) {
	if s == "" || utf8.RuneCountInString(s) > 64 {
		return "", false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return "", false
		}
	}
	return s, true
}

// IsClientClosed reports whether err is a disconnect rather than a fault.
func IsClientClosed(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, http.ErrAbortHandler) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code == CodeClientClosed
	}
	return false
}
