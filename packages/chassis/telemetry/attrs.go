// Package telemetry sets up tracing and carries the SPEC 17.3 tenancy tuple
// onto spans.
//
// The span attribute and baggage keys are generated into keys.gen.go from
// packages/schema/observability.json, so a Python service tags a span with the
// same key a Go service does and the two halves of a trace actually join.
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
