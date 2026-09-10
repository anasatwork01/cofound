// Package scope carries the tenancy tuple that SPEC 17.3 requires on every span
// and every log line: org, project, session and turn.
//
// It deliberately depends on nothing — not otel, not slog, not net/http — so
// that logging, telemetry, errs and httpx can all read it without importing one
// another. Every attempt to give this package a dependency creates a cycle
// somewhere else; that is what it is for.
package scope

import "context"

// Scope is the tenancy tuple. The zero value is meaningful: it means "not yet
// resolved", which is the state of every request before auth runs.
type Scope struct {
	OrgID     string
	ProjectID string
	SessionID string
	Turn      int
}

// Merge returns s with o's non-zero fields applied. Later resolution wins, but
// cannot erase what an earlier stage already established — auth resolves the org
// before routing knows the session, and neither should clear the other.
func (s Scope) Merge(o Scope) Scope {
	if o.OrgID != "" {
		s.OrgID = o.OrgID
	}
	if o.ProjectID != "" {
		s.ProjectID = o.ProjectID
	}
	if o.SessionID != "" {
		s.SessionID = o.SessionID
	}
	if o.Turn != 0 {
		s.Turn = o.Turn
	}
	return s
}

// IsZero reports whether nothing has been resolved yet.
func (s Scope) IsZero() bool { return s == Scope{} }

type scopeKey struct{}

type requestIDKey struct{}

// With merges s into any scope already in ctx. It never replaces: a later stage
// adding the session id must not drop the org id resolved by auth.
func With(ctx context.Context, s Scope) context.Context {
	if s.IsZero() {
		return ctx
	}
	return context.WithValue(ctx, scopeKey{}, From(ctx).Merge(s))
}

// From returns the scope in ctx, or the zero Scope.
func From(ctx context.Context) Scope {
	if s, ok := ctx.Value(scopeKey{}).(Scope); ok {
		return s
	}
	return Scope{}
}

// WithRequestID stores the request id. It is separate from Scope because it
// exists for every request, including those that never resolve a tenant.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the request id in ctx, or "".
func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}
