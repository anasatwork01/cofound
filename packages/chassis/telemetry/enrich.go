package telemetry

import (
	"context"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"

	"github.com/anasatwork01/cofound/packages/chassis/scope"
)

// Attrs converts a scope to span attributes. Pure; empties are skipped.
func Attrs(s scope.Scope) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, 4)
	if s.OrgID != "" {
		out = append(out, KeyOrgID.String(s.OrgID))
	}
	if s.ProjectID != "" {
		out = append(out, KeyProjectID.String(s.ProjectID))
	}
	if s.SessionID != "" {
		out = append(out, KeySessionID.String(s.SessionID))
	}
	if s.Turn != 0 {
		// Int, not String: a turn is a number and a backend should be able to
		// range over it.
		out = append(out, KeyTurn.Int(s.Turn))
	}
	return out
}

// Enrich attaches the tenancy tuple to the span already in ctx and puts it in
// baggage so it crosses into sandboxd and the sandbox.
//
// It is called AFTER otelhttp has started and sampled the span, because org and
// project are only known once auth has resolved. Two consequences worth
// knowing:
//
//   - Attributes added after span start cannot influence a head sampling
//     decision. "Always sample org X" is therefore impossible with head
//     sampling and needs tail sampling in the collector.
//   - Enriching an ended or non-recording span fails silently. A handler that
//     spawns a goroutine and enriches after returning produces spans
//     mysteriously missing every halyard.* attribute.
func Enrich(ctx context.Context, s scope.Scope) context.Context {
	if s.IsZero() {
		return ctx
	}
	if attrs := Attrs(s); len(attrs) > 0 {
		trace.SpanFromContext(ctx).SetAttributes(attrs...)
	}

	members := make([]baggage.Member, 0, 4)
	add := func(k, v string) {
		if v == "" {
			return
		}
		if m, err := baggage.NewMember(k, v); err == nil {
			members = append(members, m)
		}
	}
	add(BaggageOrgID, s.OrgID)
	add(BaggageProjectID, s.ProjectID)
	add(BaggageSessionID, s.SessionID)
	if s.Turn != 0 {
		add(BaggageTurn, strconv.Itoa(s.Turn))
	}
	if len(members) == 0 {
		return ctx
	}

	existing := baggage.FromContext(ctx)
	for _, m := range members {
		next, err := existing.SetMember(m)
		if err != nil {
			continue
		}
		existing = next
	}
	return baggage.ContextWithBaggage(ctx, existing)
}

// ScopeFromBaggage reads the tuple a caller sent.
//
// TELEMETRY ONLY. Baggage is unauthenticated input and the sandbox is untrusted
// (SPEC 17): code running there can forge halyard.org.id for another org. An
// authorization decision takes org_id and project_id from auth resolution,
// never from here. This doc comment is the guard rail — the compiler cannot
// tell the two uses apart.
func ScopeFromBaggage(ctx context.Context) scope.Scope {
	b := baggage.FromContext(ctx)
	s := scope.Scope{
		OrgID:     b.Member(BaggageOrgID).Value(),
		ProjectID: b.Member(BaggageProjectID).Value(),
		SessionID: b.Member(BaggageSessionID).Value(),
	}
	if turn := b.Member(BaggageTurn).Value(); turn != "" {
		if n, err := strconv.Atoi(turn); err == nil {
			s.Turn = n
		}
	}
	return s
}
