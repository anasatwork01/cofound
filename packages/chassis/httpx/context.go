package httpx

import (
	"context"
	"net/netip"

	"github.com/anasatwork01/cofound/packages/chassis/lifecycle"
)

type (
	drainKey    struct{}
	clientIPKey struct{}
	routeKey    struct{}
)

// WithDrain seeds the drain into a request context. Called from BaseContext, so
// every request carries it without a middleware.
func WithDrain(ctx context.Context, d *lifecycle.Drain) context.Context {
	return context.WithValue(ctx, drainKey{}, d)
}

// DrainFrom returns the drain in ctx. Never nil: a nil *lifecycle.Drain never
// fires, so a handler running outside the chassis stack needs no nil check.
func DrainFrom(ctx context.Context) *lifecycle.Drain {
	if d, ok := ctx.Value(drainKey{}).(*lifecycle.Drain); ok {
		return d
	}
	return nil
}

func withWriter(ctx context.Context, w *Writer) context.Context {
	return context.WithValue(ctx, writerKey{}, w)
}

// WriterFrom returns the recording writer, or nil outside the chassis stack.
func WriterFrom(ctx context.Context) *Writer {
	if w, ok := ctx.Value(writerKey{}).(*Writer); ok {
		return w
	}
	return nil
}

func withClientIP(ctx context.Context, ip netip.Addr) context.Context {
	return context.WithValue(ctx, clientIPKey{}, ip)
}

// ClientIPFrom returns the resolved client address, or the zero Addr.
func ClientIPFrom(ctx context.Context) netip.Addr {
	if ip, ok := ctx.Value(clientIPKey{}).(netip.Addr); ok {
		return ip
	}
	return netip.Addr{}
}

func withRoute(ctx context.Context, route string) context.Context {
	return context.WithValue(ctx, routeKey{}, route)
}

// RouteFrom returns the matched route pattern, or "" on a 404 or 405.
func RouteFrom(ctx context.Context) string {
	if s, ok := ctx.Value(routeKey{}).(string); ok {
		return s
	}
	return ""
}
