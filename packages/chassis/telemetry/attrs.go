// Package telemetry sets up tracing and carries the SPEC 17.3 tenancy tuple
// onto spans.
//
// Two properties are load-bearing and easy to lose:
//
//   - It boots with no collector. OTEL_SDK_DISABLED is NOT implemented anywhere
//     in the Go SDK, so disabled mode is implemented here: an empty endpoint
//     yields a no-op TracerProvider. A chassis that required a collector to
//     start would be hostile in local development and in CI.
//   - The composite propagator is installed in EVERY mode, including no-op, so
//     an inbound traceparent still passes through a service with tracing off
//     and outbound hops keep carrying it.
package telemetry

import "go.opentelemetry.io/otel/attribute"

// Span attribute keys.
//
// Dotted segments follow OTel convention: an entity's id gets its own segment,
// as in service.instance.id, and snake_case is reserved for multi-word leaves.
// So halyard.org.id, not halyard.org_id.
const (
	KeyOrgID      = attribute.Key("halyard.org.id")
	KeyProjectID  = attribute.Key("halyard.project.id")
	KeySessionID  = attribute.Key("halyard.session.id")
	KeyTurn       = attribute.Key("halyard.turn")
	KeyRequestID  = attribute.Key("halyard.request.id")
	KeyErrorCode  = attribute.Key("halyard.error.code")
	KeyStreamKind = attribute.Key("halyard.stream.kind")
)

// Baggage member keys. Identical strings to the span attributes, named
// separately so a future divergence is expressible without a silent rename.
const (
	BaggageOrgID     = "halyard.org.id"
	BaggageProjectID = "halyard.project.id"
	BaggageSessionID = "halyard.session.id"
	BaggageTurn      = "halyard.turn"
)
