package telemetry

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// There are two constructors rather than one plus a comment, because
// otelhttp.NewTransport injects the baggage header on EVERY request made with
// that client. A single shared traced client would send halyard.org.id and
// halyard.session.id to Stripe, Google Ads, Meta and the model providers —
// verified: a plain httptest call carried
// "baggage: halyard.turn=7,halyard.org.id=org_1". OTel's own documentation says
// not to put sensitive information in baggage; a tenant id crossing our
// perimeter to a payment processor qualifies.

// Internal is for authenticated control-plane hops: api to sandboxd, sandboxd
// to gitd. Injects traceparent and baggage.
func Internal(tp trace.TracerProvider, base http.RoundTripper) *http.Client {
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{
		Transport: otelhttp.NewTransport(base,
			otelhttp.WithTracerProvider(tp),
			otelhttp.WithPropagators(Propagator()),
		),
	}
}

// Vendor is for Stripe, the ad platforms and the model providers. Injects
// traceparent only, so no halyard.* baggage leaves our perimeter.
func Vendor(tp trace.TracerProvider, base http.RoundTripper) *http.Client {
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{
		Transport: otelhttp.NewTransport(StripBaggage(base),
			otelhttp.WithTracerProvider(tp),
			// TraceContext only: no Baggage propagator, so nothing to inject.
			otelhttp.WithPropagators(propagation.TraceContext{}),
		),
	}
}

// StripBaggage removes the baggage header from an outbound request. Belt and
// braces alongside the propagator choice: a caller that sets the header by hand
// still cannot leak through a vendor client.
func StripBaggage(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("baggage") != "" || r.Header.Get("Baggage") != "" {
			r = r.Clone(r.Context())
			r.Header.Del("baggage")
			r.Header.Del("Baggage")
		}
		return next.RoundTrip(r)
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
