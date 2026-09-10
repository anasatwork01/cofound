package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/chassis/logkey"
)

// LogFields seeds the route into the context after routing is known, so the
// access log and every handler log line can name it.
func LogFields(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// chi populates the pattern during dispatch, so a wrapper is needed to
		// read it at the point the inner handler runs.
		next.ServeHTTP(w, r.WithContext(withRouteResolver(r)))
	})
}

func withRouteResolver(r *http.Request) context.Context {
	if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
		return withRoute(r.Context(), rc.RoutePattern())
	}
	return r.Context()
}

// AccessLog emits one line per request from a defer, so it also fires while a
// panic unwinds — which is why it sits OUTSIDE Recover.
//
// It logs the chi route pattern, never the raw path: a raw path is unbounded
// cardinality and can carry a token or a user-chosen slug.
func AccessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			defer func() {
				status := 0
				var bytes int64
				if ww, ok := WriterFromChain(w); ok {
					status, bytes = ww.Status(), ww.Bytes()
				}

				route := RouteFrom(r.Context())
				if route == "" {
					if rc := chi.RouteContext(r.Context()); rc != nil {
						route = rc.RoutePattern()
					}
				}
				if route == "" {
					route = "unmatched"
				}

				attrs := []slog.Attr{
					slog.String(logkey.HTTPMethod, r.Method),
					slog.String(logkey.HTTPRoute, route),
					slog.Int(logkey.HTTPStatus, status),
					slog.Int64(logkey.HTTPBytes, bytes),
					slog.Int64(logkey.DurationMS, time.Since(start).Milliseconds()),
				}
				if ip := ClientIPFrom(r.Context()); ip.IsValid() {
					attrs = append(attrs, slog.String(logkey.ClientIP, ip.String()))
				}

				level := slog.LevelInfo
				switch {
				case status >= 500 || status == 0:
					level = slog.LevelError
				case status >= 400:
					level = slog.LevelWarn
				}
				log.LogAttrs(r.Context(), level, "http request", attrs...)
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP resolves the client address.
//
// X-Forwarded-For is honoured only when the immediate peer is inside a
// configured CIDR. With none configured the header is ignored entirely: the
// default is not to believe a header, because a spoofed client ip poisons rate
// limiting and audit records.
func ClientIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, ok := peerAddr(r)
			if ok && len(trusted) > 0 &&
				slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(ip) }) {
				if fwd := forwardedFor(r, trusted); fwd.IsValid() {
					ip = fwd
				}
			}
			if !ok && !ip.IsValid() {
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(withClientIP(r.Context(), ip)))
		})
	}
}

// forwardedFor walks X-Forwarded-For from the right, skipping addresses that
// are themselves trusted proxies, and returns the first that is not. Taking
// the leftmost entry instead would let a client prepend anything it liked.
func forwardedFor(r *http.Request, trusted []netip.Prefix) netip.Addr {
	raw := r.Header.Get("X-Forwarded-For")
	if raw == "" {
		return netip.Addr{}
	}
	parts := strings.Split(raw, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			continue
		}
		ip = ip.Unmap()
		if slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(ip) }) {
			continue
		}
		return ip
	}
	return netip.Addr{}
}

// logError records a failure on the log at the right level, with the cause and
// — for a 5xx — the stack. This is the only place a cause is allowed to be
// rendered, and it is not a response.
func logError(ctx context.Context, log *slog.Logger, e *errs.Error, requestID string) {
	attrs := []slog.Attr{
		slog.String(logkey.ErrorCode, string(e.Code)),
		slog.Int(logkey.HTTPStatus, e.Status),
		slog.String(logkey.Err, e.Error()),
	}
	if requestID != "" {
		attrs = append(attrs, slog.String(logkey.RequestID, requestID))
	}

	level := slog.LevelWarn
	switch {
	case e.Code == errs.CodeClientClosed:
		// A disconnect is not a fault, and counting it as a 5xx would corrupt
		// the availability SLO.
		level = slog.LevelInfo
	case e.Status >= 500:
		level = slog.LevelError
		if stack := e.Stack(); stack != "" {
			attrs = append(attrs, slog.String(logkey.Stack, stack))
		}
	case e.Code == errs.CodeNotFound:
		// A 404 is an expected outcome, not an incident.
		level = slog.LevelInfo
	}
	log.LogAttrs(ctx, level, "request failed", attrs...)
}
