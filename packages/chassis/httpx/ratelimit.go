package httpx

import (
	"math"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/chassis/scope"
)

// RateLimit is the shape of a limit: a sustained rate, and how much of it can
// be spent at once.
//
// A token bucket rather than a fixed window, for one reason that matters in
// practice: a fixed window lets a caller spend the whole allowance in the last
// millisecond of one window and the whole of the next in the first
// millisecond, which is twice the intended rate at exactly the moment a
// thundering herd forms. A bucket cannot be made to do that.
type RateLimit struct {
	// Rate is sustained requests per second. Fractional is fine and useful:
	// 0.2 is one request every five seconds.
	Rate float64
	// Burst is the bucket's capacity. Equal to Rate means no burst at all,
	// which is usually too strict — a console page load is a dozen requests in
	// a few hundred milliseconds and is not abuse.
	Burst float64
}

// RateLimitStore holds bucket state.
//
// An interface so the in-process implementation below can be swapped for a
// shared one without touching the middleware. That swap is not hypothetical: an
// in-process limiter divides the real limit by the number of replicas, which is
// fine for protecting one process and wrong for enforcing a per-tenant quota.
// SPEC §21 decision 1 (the container host) determines what the shared store
// should be, so it is deliberately not chosen here.
type RateLimitStore interface {
	// Take removes one token, returning whether it was available and how long
	// until the next one is.
	Take(key string, limit RateLimit, now time.Time) (allowed bool, retryAfter time.Duration)
}

// RateLimiter refuses requests over a limit.
type RateLimiter struct {
	// Limit applied per Key.
	Limit RateLimit
	// Key identifies the caller. Returning "" exempts a request entirely.
	Key func(*http.Request) string
	// Store defaults to an in-process one.
	Store RateLimitStore
	// Now is injected for tests.
	Now func() time.Time

	Errors *ErrorWriter
}

// Middleware applies the limit.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	store := rl.Store
	if store == nil {
		store = NewMemoryRateLimitStore()
	}
	now := rl.Now
	if now == nil {
		now = time.Now
	}
	ew := rl.Errors
	if ew == nil {
		ew = NewErrorWriter(nil)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := ""
		if rl.Key != nil {
			key = rl.Key(r)
		}
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}
		allowed, retryAfter := store.Take(key, rl.Limit, now())
		if allowed {
			next.ServeHTTP(w, r)
			return
		}
		// Retry-After, always. SPEC §18: an error says what to do about it, and
		// "too many requests" without a number leaves a client guessing — which
		// in practice means retrying immediately and making it worse.
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		ew.Fail(w, r, errs.RateLimited(retryAfter))
	})
}

// KeyByOrg limits per tenant, falling back to the peer address.
//
// Reading the org from the chassis scope rather than from a header: the scope
// was resolved from a verified session, and a header is whatever the caller
// typed. The fallback matters because the endpoints most worth limiting —
// sign-in, magic links — run before any org exists.
func KeyByOrg(r *http.Request) string {
	if org := scope.From(r.Context()).OrgID; org != "" {
		return "org:" + org
	}
	return KeyByIP(r)
}

// KeyByIP limits per peer, for the unauthenticated surface.
//
// The peer the chassis's ClientIP middleware resolved, which has already
// applied the TRUSTED_PROXY_CIDRS policy. Reading X-Forwarded-For here would
// trust a header the chassis deliberately does not — and a rate limiter keyed
// on a spoofable value is one an attacker can key to someone else.
func KeyByIP(r *http.Request) string {
	if ip := ClientIPFrom(r.Context()); ip.IsValid() {
		return "ip:" + ip.String()
	}
	// The ClientIP middleware has not run — this limiter was mounted outside
	// the chassis router, or ahead of it. Fall back to the TCP peer.
	//
	// The first version returned a single constant here, which put every caller
	// in ONE bucket: correct-looking, and in production it means one abusive
	// client rate-limits everybody. A test with two callers caught it. The peer
	// address is the honest fallback — it cannot be spoofed the way a header
	// can, and at worst a shared proxy shares a bucket, which is the same
	// property the trusted-proxy policy exists to refine.
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "" {
		return "ip:unknown"
	}
	return "ip:" + host
}

// memoryRateLimitStore is a sharded in-process token bucket.
//
// Sharded because a single mutex over every tenant's bucket is a global lock on
// the request path, and this middleware runs on every request by definition.
type memoryRateLimitStore struct {
	shards [64]struct {
		mu      sync.Mutex
		buckets map[string]*bucket
	}
	// lastSweep bounds eviction work: buckets are dropped when they have been
	// full and idle long enough to be indistinguishable from a new one, which
	// is what stops the map growing with every IP that ever connected.
	lastSweep time.Time
	sweepMu   sync.Mutex
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewMemoryRateLimitStore returns an in-process store.
func NewMemoryRateLimitStore() RateLimitStore {
	s := &memoryRateLimitStore{}
	for i := range s.shards {
		s.shards[i].buckets = map[string]*bucket{}
	}
	return s
}

func (s *memoryRateLimitStore) Take(key string, limit RateLimit, now time.Time) (bool, time.Duration) {
	if limit.Rate <= 0 {
		return true, 0
	}
	capacity := limit.Burst
	if capacity < 1 {
		capacity = 1
	}

	sh := &s.shards[fnv(key)%uint32(len(s.shards))]
	sh.mu.Lock()
	b, ok := sh.buckets[key]
	if !ok {
		b = &bucket{tokens: capacity, last: now}
		sh.buckets[key] = b
	}
	// Refill for the elapsed time, capped at capacity.
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens = math.Min(capacity, b.tokens+elapsed*limit.Rate)
		b.last = now
	}
	allowed := b.tokens >= 1
	var retry time.Duration
	if allowed {
		b.tokens--
	} else {
		retry = time.Duration((1 - b.tokens) / limit.Rate * float64(time.Second))
	}
	sh.mu.Unlock()

	s.maybeSweep(now, capacity, limit.Rate)
	return allowed, retry
}

// maybeSweep drops buckets that have refilled to capacity and gone idle.
//
// Such a bucket is indistinguishable from a brand-new one, so forgetting it
// changes no decision. Without this the map keeps an entry for every key ever
// seen, which for an IP-keyed limiter is every client that ever connected.
func (s *memoryRateLimitStore) maybeSweep(now time.Time, capacity, rate float64) {
	const every = time.Minute
	s.sweepMu.Lock()
	if now.Sub(s.lastSweep) < every {
		s.sweepMu.Unlock()
		return
	}
	s.lastSweep = now
	s.sweepMu.Unlock()

	// Idle long enough to have refilled completely, twice over.
	idle := time.Duration(capacity/rate*2) * time.Second
	if idle < every {
		idle = every
	}
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		for k, b := range sh.buckets {
			if now.Sub(b.last) > idle {
				delete(sh.buckets, k)
			}
		}
		sh.mu.Unlock()
	}
}

func fnv(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}
