package httpx

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// CORSConfig describes the one browser origin a service answers.
//
// Singular on purpose. SPEC §7.1 has exactly one first-party browser client —
// the console — and every extra origin is another host that can drive an
// authenticated session. A list invites "just add localhost" to reach
// production, so widening this is a deliberate edit rather than configuration.
type CORSConfig struct {
	// AllowOrigin is matched EXACTLY against the request's Origin and echoed
	// back unchanged. Empty disables CORS entirely, which is correct for a
	// service no browser talks to (gitd, aigw, mcp).
	AllowOrigin string

	// AllowMethods and AllowHeaders answer a preflight. Both have sane
	// defaults; a service only sets them to add something unusual.
	AllowMethods []string
	AllowHeaders []string

	// MaxAge caps how long a browser may cache a preflight. Chromium clamps to
	// 2 hours regardless, so anything larger is aspirational.
	MaxAge time.Duration
}

var (
	defaultCORSMethods = []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"}

	// Idempotency-Key is task 0.10's, X-Halyard-Org is task 0.8's answer to Q6.
	// Both are sent by the console on ordinary requests, and a header the
	// preflight does not name is one the browser refuses to send.
	defaultCORSHeaders = []string{
		"Content-Type", "Accept", "Idempotency-Key", "X-Halyard-Org", "Last-Event-ID",
	}
)

// CORS answers cross-origin browser requests for exactly one origin.
//
// MOUNT IT ON THE ROOT, OUTSIDE THE AUTHED SUBTREE. A preflight is sent
// WITHOUT cookies by every browser, so an OPTIONS that reaches authentication
// middleware is answered 401, and a 401 preflight fails the actual request —
// which presents as "CORS blocked" with no clue that auth was involved. This
// middleware short-circuits OPTIONS before anything downstream runs.
//
// Two rules here are security properties rather than configuration:
//
//   - The origin is COMPARED and then echoed, never reflected. Echoing back
//     whatever arrived turns any site into a first-party caller of an
//     authenticated API, which is the standard CORS vulnerability and would
//     undo SPEC §17's boundary from the browser side.
//   - `*` is never sent. It is illegal alongside
//     Access-Control-Allow-Credentials, and the console's fetches set
//     `credentials: "include"` because SPEC §8's session is an httpOnly cookie.
//     A browser rejects the pair, so a wildcard here would not merely be loose,
//     it would not work.
//
// `Vary: Origin` is set on every response, including same-origin ones, so a
// shared cache cannot serve a response with one origin's ACAO header to a
// different origin.
func CORS(cfg CORSConfig) func(http.Handler) http.Handler {
	if cfg.AllowOrigin == "" {
		// No browser client: every request passes through untouched, and no
		// CORS header is emitted at all.
		return func(next http.Handler) http.Handler { return next }
	}

	methods := cfg.AllowMethods
	if len(methods) == 0 {
		methods = defaultCORSMethods
	}
	headers := cfg.AllowHeaders
	if len(headers) == 0 {
		headers = defaultCORSHeaders
	}
	maxAge := cfg.MaxAge
	if maxAge <= 0 {
		maxAge = 2 * time.Hour
	}

	allowMethods := strings.Join(methods, ", ")
	allowHeaders := strings.Join(headers, ", ")
	maxAgeSeconds := strconv.Itoa(int(maxAge.Seconds()))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Always, even when the origin does not match and even on
			// same-origin requests: the response varies by Origin either way,
			// and a cache that does not know it will serve the wrong one.
			w.Header().Add("Vary", "Origin")

			origin := r.Header.Get("Origin")
			if origin == "" {
				// Not a browser cross-origin request. curl, a health probe, a
				// server-to-server call. Nothing to negotiate.
				next.ServeHTTP(w, r)
				return
			}
			if origin != cfg.AllowOrigin {
				// Deliberately NOT an error: the request proceeds and the
				// browser blocks it for want of the header. Failing loudly here
				// would hand an attacker a probe for which origins are
				// configured, and would break non-browser callers that send an
				// Origin for their own reasons.
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")

			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				// A real preflight. Answer it here and go no further: the
				// handler for this path may require a session, and the
				// preflight has no cookie to offer.
				h.Set("Access-Control-Allow-Methods", allowMethods)
				h.Set("Access-Control-Allow-Headers", allowHeaders)
				h.Set("Access-Control-Max-Age", maxAgeSeconds)
				h.Add("Vary", "Access-Control-Request-Method")
				h.Add("Vary", "Access-Control-Request-Headers")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
