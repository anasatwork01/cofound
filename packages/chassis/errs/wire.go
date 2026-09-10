package errs

import (
	"errors"
	"maps"

	"github.com/anasatwork01/cofound/packages/schema/gen/go/agentevents"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/common"
)

// This file is the ONLY place in the repository that constructs a
// common.Error. Everything client-visible passes through here, which is what
// makes the encode-side invariants enforceable at all.
//
// Verified: the generated common.Error has UnmarshalJSON validating code
// against ^[a-z][a-z0-9_]*$ and message against minLength 1 — on DECODE only.
// There is no MarshalJSON, so nothing validates on the way out. An invalid code
// or empty message would serialise happily and then fail inside every client's
// decoder, at the exact moment something is already broken. So Wire degrades
// rather than emitting one.

// fallbackMessage replaces an empty authored message.
const fallbackMessage = "Halyard could not complete this request."

// Wire renders e for the HTTP envelope.
//
// The body is built only from authored Message and Fix strings. There is no
// path from e.cause to any field here — that is the leak-proofing, and it is
// structural rather than a rule to remember.
func (e *Error) Wire(requestID string) common.Error {
	code := e.Code
	if !code.Valid() {
		code = CodeInternal
	}
	msg := e.Message
	if msg == "" {
		msg = fallbackMessage
	}

	out := common.Error{
		Code:      string(code),
		Message:   msg,
		Retriable: e.Retriable,
	}

	// Fix and RequestId address LOCALS. Taking the address of a field on a
	// possibly-shared *Error would alias it into the response.
	if e.Fix != "" {
		fix := e.Fix
		out.Fix = &fix
	}
	if requestID != "" {
		id := requestID
		out.RequestId = &id
	}
	if len(e.Details) > 0 {
		out.Details = common.ErrorDetails(maps.Clone(e.Details))
	}
	return out
}

// Response wraps Wire in the envelope every endpoint returns.
func (e *Error) Response(requestID string) common.ErrorResponse {
	return common.ErrorResponse{Error: e.Wire(requestID)}
}

// Event renders e as an agent-events error frame, for a failure after an SSE
// stream has already opened and a status code is no longer available.
//
// go-jsonschema inlined the same Error definition into the agentevents package
// and each package owns its own ErrorDetails type, so common.Error and
// agentevents.ErrorEvent are NOT convertible. This is the one place that
// conversion lives, so nobody hand-writes a field copy that drifts.
// agentevents.ErrorEvent carries only Code, Message, Retriable, Turn and Type
// — no fix, no details, no request id.
func (e *Error) Event(turn int) agentevents.ErrorEvent {
	code := e.Code
	if !code.Valid() {
		code = CodeInternal
	}
	msg := e.Message
	if msg == "" {
		msg = fallbackMessage
	}
	return agentevents.ErrorEvent{
		Type:      agentevents.ErrorEventTypeError,
		Turn:      turn,
		Code:      string(code),
		Message:   msg,
		Retriable: e.Retriable,
	}
}

// From is the leak-proofing entry point: a typed error passes through, and
// anything else becomes an internal fault whose cause reaches only the log.
//
// Every rendering path calls this, so a handler returning a raw driver error
// produces a canned envelope by construction rather than by review.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	if IsClientClosed(err) {
		return ClientClosed(err)
	}
	return Internal(err)
}
