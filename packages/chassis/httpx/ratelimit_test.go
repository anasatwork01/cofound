package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/httpx"
)

// clock is manually advanced, so the refill tests do not sleep.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func limiter(limit httpx.RateLimit, key func(*http.Request) string, c *clock) http.Handler {
	rl := &httpx.RateLimiter{Limit: limit, Key: key, Now: c.now}
	return rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func hit(h http.Handler, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = key + ":1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestBurstIsAllowedThenRefused(t *testing.T) {
	t.Parallel()
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	h := limiter(httpx.RateLimit{Rate: 1, Burst: 3}, httpx.KeyByIP, c)

	for i := range 3 {
		if code := hit(h, "203.0.113.1").Code; code != http.StatusOK {
			t.Fatalf("request %d in the burst = %d, want 200", i, code)
		}
	}
	rec := hit(h, "203.0.113.1")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the fourth request = %d, want 429", rec.Code)
	}
	// SPEC §18: an error says what to do about it. "Too many requests" with no
	// number leaves a client guessing, which in practice means retrying
	// immediately and making it worse.
	if rec.Header().Get("Retry-After") == "" {
		t.Error("a 429 must carry Retry-After")
	}
}

func TestTheBucketRefillsOverTime(t *testing.T) {
	t.Parallel()
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	h := limiter(httpx.RateLimit{Rate: 2, Burst: 2}, httpx.KeyByIP, c)

	hit(h, "203.0.113.2")
	hit(h, "203.0.113.2")
	if hit(h, "203.0.113.2").Code != http.StatusTooManyRequests {
		t.Fatal("the bucket should be empty")
	}

	// Half a second at two per second is one token.
	c.t = c.t.Add(500 * time.Millisecond)
	if code := hit(h, "203.0.113.2").Code; code != http.StatusOK {
		t.Errorf("after refilling one token: %d, want 200", code)
	}
	if hit(h, "203.0.113.2").Code != http.StatusTooManyRequests {
		t.Error("only one token should have refilled")
	}
}

// TestTheBucketDoesNotRefillPastItsCapacity.
//
// This is the property a fixed window lacks: without a cap, an idle caller
// accumulates an unbounded allowance and can spend it all at once, which is
// exactly when a thundering herd forms.
func TestTheBucketDoesNotRefillPastItsCapacity(t *testing.T) {
	t.Parallel()
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	h := limiter(httpx.RateLimit{Rate: 10, Burst: 3}, httpx.KeyByIP, c)

	// Idle for an hour.
	c.t = c.t.Add(time.Hour)
	allowed := 0
	for range 20 {
		if hit(h, "203.0.113.3").Code == http.StatusOK {
			allowed++
		}
	}
	if allowed != 3 {
		t.Errorf("an hour idle allowed %d immediate requests, want the burst of 3", allowed)
	}
}

// TestOneCallerCannotExhaustAnother. The point of keying at all.
func TestOneCallerCannotExhaustAnother(t *testing.T) {
	t.Parallel()
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	h := limiter(httpx.RateLimit{Rate: 1, Burst: 2}, httpx.KeyByIP, c)

	for range 5 {
		hit(h, "203.0.113.4")
	}
	if code := hit(h, "203.0.113.5").Code; code != http.StatusOK {
		t.Errorf("a second caller was refused at %d after the first exhausted its bucket", code)
	}
}

// TestAnEmptyKeyExempts, so a route can opt out by returning "".
func TestAnEmptyKeyExempts(t *testing.T) {
	t.Parallel()
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	h := limiter(httpx.RateLimit{Rate: 1, Burst: 1}, func(*http.Request) string { return "" }, c)
	for i := range 10 {
		if code := hit(h, "203.0.113.6").Code; code != http.StatusOK {
			t.Fatalf("exempt request %d was refused with %d", i, code)
		}
	}
}

// TestAZeroRateIsUnlimited, so a limit left unconfigured does not silently
// refuse everything.
func TestAZeroRateIsUnlimited(t *testing.T) {
	t.Parallel()
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	h := limiter(httpx.RateLimit{}, httpx.KeyByIP, c)
	for i := range 50 {
		if code := hit(h, "203.0.113.7").Code; code != http.StatusOK {
			t.Fatalf("request %d refused under a zero rate: %d", i, code)
		}
	}
}

// TestTheRefusalIsTheWireEnvelope, so a generated client can decode it.
func TestTheRefusalIsTheWireEnvelope(t *testing.T) {
	t.Parallel()
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	h := limiter(httpx.RateLimit{Rate: 1, Burst: 1}, httpx.KeyByIP, c)
	hit(h, "203.0.113.8")
	rec := hit(h, "203.0.113.8")

	if ct := rec.Header().Get("Content-Type"); ct == "" {
		t.Error("no content type on a 429")
	}
	body := rec.Body.String()
	for _, want := range []string{`"code":"rate_limited"`, `"retriable":true`} {
		if !contains(body, want) {
			t.Errorf("the 429 body is missing %s: %s", want, body)
		}
	}
}

func contains(h, n string) bool {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return true
		}
	}
	return false
}
