// Package logkey is the closed vocabulary of structured log field names.
//
// It exists so that a rename is one edit rather than a grep, and so that the
// redaction audit test has something finite to iterate: every key here is
// asserted not to be classified as a secret, which is what stops the denylist's
// substring matching from silently swallowing a field a debugger needed.
package logkey

// Process identity.
const (
	Service  = "service"
	Env      = "env"
	Version  = "version"
	Commit   = "commit"
	Instance = "instance_id"
)

// Correlation. SPEC 17.3 wants a trace spanning five hops; these are the fields
// that let a log search join onto it.
const (
	RequestID = "request_id"
	TraceID   = "trace_id"
	SpanID    = "span_id"
	Sampled   = "trace_sampled"
)

// Tenancy (SPEC 17.3).
const (
	OrgID     = "org_id"
	ProjectID = "project_id"
	SessionID = "session_id"
	Turn      = "turn"
)

// Failure.
const (
	ErrorCode  = "error_code"
	Err        = "error"
	PanicValue = "panic_value"
	Stack      = "stack"
)

// HTTP. Route, never path: a raw path is unbounded cardinality and can carry a
// token or a user-chosen slug.
const (
	HTTPMethod = "http_method"
	HTTPRoute  = "http_route"
	HTTPStatus = "http_status"
	HTTPBytes  = "http_bytes"
	DurationMS = "duration_ms"
	ClientIP   = "client_ip"
)

// General.
const (
	Component = "component"
	Reason    = "reason"
	Phase     = "phase"
	Count     = "count"
)

// Streams and shutdown.
const (
	StreamKind       = "stream_kind"
	StreamsOpen      = "streams_open"
	StreamsRemaining = "streams_remaining"
	Clean            = "clean"
)

// Readiness.
const (
	Probe      = "probe"
	ProbeState = "probe_state"
	ProbeSince = "probe_since"
	LatencyMS  = "latency_ms"
)

// Boot.
const (
	Addr      = "addr"
	ConfigKey = "config_key"
)

// All returns every key in the vocabulary. The redaction audit test iterates it,
// so a key added above is automatically covered.
func All() []string {
	return []string{
		Service, Env, Version, Commit, Instance,
		RequestID, TraceID, SpanID, Sampled,
		OrgID, ProjectID, SessionID, Turn,
		ErrorCode, Err, PanicValue, Stack,
		HTTPMethod, HTTPRoute, HTTPStatus, HTTPBytes, DurationMS, ClientIP,
		Component, Reason, Phase, Count,
		StreamKind, StreamsOpen, StreamsRemaining, Clean,
		Probe, ProbeState, ProbeSince, LatencyMS,
		Addr, ConfigKey,
	}
}
