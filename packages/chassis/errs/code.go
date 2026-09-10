// Package errs is the chassis error vocabulary and the only place in the
// repository that constructs the generated wire error type.
//
// The safety property is structural, not disciplinary. A handler returns error
// and has no way to render one itself; the single adapter maps anything that is
// not an *errs.Error to a canned internal envelope, and the wire body is built
// only from authored Message and Fix strings — never from err.Error(). So a
// handler that returns a raw pgx error cannot leak a connection string, because
// there is no code path from a cause to a response body.
package errs

import "regexp"

// Code is a stable, machine-readable error identifier.
//
// It is a string type that is itself an error, which is what makes
// errors.Is(err, errs.CodeNotFound) work without declaring a sentinel value per
// code.
type Code string

// Error implements error.
func (c Code) Error() string { return string(c) }

// codePattern is the pattern the GENERATED decoder enforces on the wire. A code
// that fails it decodes as an error in every client, so Wire degrades rather
// than emitting one.
var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Valid reports whether c satisfies the wire contract.
func (c Code) Valid() bool { return codePattern.MatchString(string(c)) }

// Transport codes owned by the chassis. Services own domain codes.
const (
	CodeInvalid              Code = "invalid"
	CodeUnauthenticated      Code = "unauthenticated"
	CodePaymentRequired      Code = "payment_required"
	CodeForbidden            Code = "forbidden"
	CodeNotFound             Code = "not_found"
	CodeMethodNotAllowed     Code = "method_not_allowed"
	CodeConflict             Code = "conflict"
	CodePayloadTooLarge      Code = "payload_too_large"
	CodeUnsupportedMediaType Code = "unsupported_media_type"
	CodeRateLimited          Code = "rate_limited"
	CodeNotReady             Code = "not_ready"
	CodeInternal             Code = "internal"
	CodeUnavailable          Code = "unavailable"
	CodeTimeout              Code = "timeout"

	// CodeClientClosed is never rendered: the client is gone, so there is
	// nobody to render for. It exists so the access log can distinguish a
	// disconnect from a fault, and so a disconnect is not counted as a 5xx.
	CodeClientClosed Code = "client_closed"
)
