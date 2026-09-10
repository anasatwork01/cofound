package httpx

import (
	"crypto/rand"
	"net"
	"net/http"
	"net/netip"
	"slices"

	"github.com/anasatwork01/cofound/packages/chassis/scope"
)

// HeaderRequestID is the request id header.
const HeaderRequestID = "X-Request-Id"

// RequestIDConfig configures generation and inbound trust.
type RequestIDConfig struct {
	// TrustInbound honours a client-supplied id. Off by default.
	TrustInbound   bool
	TrustedProxies []netip.Prefix
	// New is injected so tests get deterministic ids.
	New func() string
}

// RequestID assigns every request an id.
//
// This is deliberately not chi's middleware.RequestID, which copies
// X-Request-Id from the client with no length or charset validation. In a
// multi-tenant control plane that hands an attacker the value that appears in
// the JSON error envelope and in every log line for the request: log injection
// via newlines, log forging by reusing another request's id, and unbounded
// field length.
//
// It is also not the OTel trace id. Under head sampling most trace ids resolve
// to nothing in the backend, so support would get an id that leads nowhere;
// and traceparent is caller-chosen at a public edge, so ids could be forced to
// collide. trace_id and span_id are separate log fields, and the join happens
// in the log where it costs nothing.
func RequestID(cfg RequestIDConfig) func(http.Handler) http.Handler {
	gen := cfg.New
	if gen == nil {
		gen = NewRequestID
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := ""
			if cfg.TrustInbound {
				if candidate := r.Header.Get(HeaderRequestID); ValidRequestID(candidate) &&
					peerIsTrusted(r, cfg.TrustedProxies) {
					id = candidate
				}
			}
			if id == "" {
				id = gen()
			}
			w.Header().Set(HeaderRequestID, id)
			next.ServeHTTP(w, r.WithContext(scope.WithRequestID(r.Context(), id)))
		})
	}
}

// NewRequestID returns 26 base32 characters, at least 128 bits of entropy.
func NewRequestID() string { return rand.Text() }

// ValidRequestID reports whether s is safe to reflect into a log line and a
// response body: ^[A-Za-z0-9_-]{8,64}$.
func ValidRequestID(s string) bool {
	if len(s) < 8 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func peerIsTrusted(r *http.Request, trusted []netip.Prefix) bool {
	if len(trusted) == 0 {
		return false
	}
	ip, ok := peerAddr(r)
	if !ok {
		return false
	}
	return slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(ip) })
}

func peerAddr(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}
