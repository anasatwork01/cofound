package httpx_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anasatwork01/cofound/packages/chassis/httpx"
)

const console = "https://console.halyard.dev"

// corsHandler is the middleware under test wrapped around a handler that
// records whether it ran. A preflight that reaches it has not been
// short-circuited, which is the bug this middleware exists to prevent.
func corsHandler(cfg httpx.CORSConfig, reached *bool) http.Handler {
	return httpx.CORS(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reached != nil {
			*reached = true
		}
		w.WriteHeader(http.StatusTeapot)
	}))
}

func preflight(origin, method string) *http.Request {
	r := httptest.NewRequest(http.MethodOptions, "/v1/projects", nil)
	r.Header.Set("Origin", origin)
	r.Header.Set("Access-Control-Request-Method", method)
	return r
}

// TestPreflightIsAnsweredWithoutCredentials is THE canary for this file.
//
// Every browser sends a preflight WITHOUT cookies. If it reaches the authed
// subtree it is answered 401, the browser discards the real request, and the
// developer sees "blocked by CORS policy" with nothing pointing at auth. That
// is precisely how phase 0's acceptance criterion failed.
func TestPreflightIsAnsweredWithoutCredentials(t *testing.T) {
	t.Parallel()
	reached := false
	rec := httptest.NewRecorder()
	corsHandler(httpx.CORSConfig{AllowOrigin: console}, &reached).
		ServeHTTP(rec, preflight(console, http.MethodPost))

	if reached {
		t.Fatal("the preflight reached the handler; it must be short-circuited")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != console {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, console)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want true", got)
	}
	for _, h := range []string{"Access-Control-Allow-Methods", "Access-Control-Allow-Headers", "Access-Control-Max-Age"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("%s is empty on a preflight response", h)
		}
	}
}

// The preflight must name every header the console actually sends. A header
// omitted here is a header the browser refuses to send on the real request,
// and the failure is a CORS error rather than anything mentioning the header.
func TestPreflightAllowsTheHeadersTheConsoleSends(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	corsHandler(httpx.CORSConfig{AllowOrigin: console}, nil).
		ServeHTTP(rec, preflight(console, http.MethodPost))

	allowed := rec.Header().Get("Access-Control-Allow-Headers")
	// Idempotency-Key is task 0.10's, X-Halyard-Org is 0.8's answer to Q6, and
	// Last-Event-ID keeps an SSE reconnect from losing its place.
	for _, h := range []string{"Content-Type", "Idempotency-Key", "X-Halyard-Org", "Last-Event-ID"} {
		if !strings.Contains(allowed, h) {
			t.Errorf("Access-Control-Allow-Headers = %q, missing %s", allowed, h)
		}
	}
	methods := rec.Header().Get("Access-Control-Allow-Methods")
	for _, m := range []string{"GET", "POST", "PATCH", "DELETE"} {
		if !strings.Contains(methods, m) {
			t.Errorf("Access-Control-Allow-Methods = %q, missing %s", methods, m)
		}
	}
}

// A different origin gets NO Access-Control-Allow-Origin. This is the whole
// security property: echoing back whatever arrived would make any site a
// first-party caller of an authenticated API.
func TestAnotherOriginIsNotAllowed(t *testing.T) {
	t.Parallel()
	for _, origin := range []string{
		"https://evil.example",
		// Prefix and suffix games that a naive strings.HasPrefix or
		// strings.Contains check would wave through.
		console + ".evil.example",
		"https://evil.example/" + console,
		"http://console.halyard.dev", // scheme differs
		"https://Console.Halyard.Dev",
		strings.ToUpper(console),
	} {
		reached := false
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
		r.Header.Set("Origin", origin)
		corsHandler(httpx.CORSConfig{AllowOrigin: console}, &reached).ServeHTTP(rec, r)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("origin %q got Access-Control-Allow-Origin = %q, want none", origin, got)
		}
		// Deliberately still served: failing loudly would hand an attacker a
		// probe for which origins are configured. The browser does the blocking.
		if !reached {
			t.Errorf("origin %q was rejected by the server; it should pass through unheaded", origin)
		}
	}
}

// `*` is illegal alongside Access-Control-Allow-Credentials — a browser rejects
// the pair — so a wildcard here would not merely be loose, it would not work.
func TestWildcardIsNeverEmitted(t *testing.T) {
	t.Parallel()
	for _, origin := range []string{console, "https://evil.example", "null", "*"} {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
		r.Header.Set("Origin", origin)
		corsHandler(httpx.CORSConfig{AllowOrigin: console}, nil).ServeHTTP(rec, r)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got == "*" {
			t.Errorf("origin %q got a wildcard Access-Control-Allow-Origin", origin)
		}
	}
}

// Every response varies by Origin, including ones carrying no CORS header at
// all. A shared cache that does not know this serves one origin's allowed
// response to another origin.
func TestVaryOriginIsAlwaysSet(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, origin string }{
		{"matching origin", console},
		{"other origin", "https://evil.example"},
		{"no origin at all", ""},
	} {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		corsHandler(httpx.CORSConfig{AllowOrigin: console}, nil).ServeHTTP(rec, r)

		if !slicesContainsFold(rec.Header().Values("Vary"), "Origin") {
			t.Errorf("%s: Vary = %v, want it to include Origin", tc.name, rec.Header().Values("Vary"))
		}
	}
}

// The zero value is off, which is what gitd, aigw and mcp want: no browser
// talks to them, so they emit no CORS header and not even a Vary.
func TestNoConfiguredOriginEmitsNothing(t *testing.T) {
	t.Parallel()
	reached := false
	rec := httptest.NewRecorder()
	corsHandler(httpx.CORSConfig{}, &reached).ServeHTTP(rec, preflight(console, http.MethodPost))

	if !reached {
		t.Fatal("an unconfigured service short-circuited an OPTIONS; it must pass through")
	}
	for h := range rec.Header() {
		if strings.HasPrefix(http.CanonicalHeaderKey(h), "Access-Control") {
			t.Errorf("unconfigured service emitted %s", h)
		}
	}
	if rec.Header().Get("Vary") != "" {
		t.Errorf("unconfigured service set Vary = %q", rec.Header().Get("Vary"))
	}
}

// A bare OPTIONS with no Access-Control-Request-Method is not a preflight — it
// is a client asking what a resource supports — and must reach the handler so
// chi answers with its own Allow header.
func TestBareOptionsIsNotTreatedAsAPreflight(t *testing.T) {
	t.Parallel()
	reached := false
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodOptions, "/v1/projects", nil)
	r.Header.Set("Origin", console)
	corsHandler(httpx.CORSConfig{AllowOrigin: console}, &reached).ServeHTTP(rec, r)

	if !reached {
		t.Fatal("a bare OPTIONS was short-circuited as if it were a preflight")
	}
}

// Mounted on the ROOT of a real router, a preflight for an AUTHED route is
// answered before authentication runs. This is the assertion the unit tests
// above cannot make: it proves the mount POINT is right, not just the
// middleware. If CORS is ever moved inside the authed subtree, this fails and
// the unit tests above still pass.
func TestPreflightForAnAuthedRouteSkipsAuthentication(t *testing.T) {
	t.Parallel()
	mux := httpx.Router(httpx.RouterConfig{
		Service: "api",
		Log:     slog.New(slog.DiscardHandler),
		CORS:    httpx.CORSConfig{AllowOrigin: console},
	})

	authenticated := false
	// Stands in for the api's real Authenticate: no cookie, no entry. A
	// preflight never carries one.
	mux.Authed.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authenticated = true
			if _, err := r.Cookie("halyard_session"); err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	mux.Authed.Post("/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	srv := httptest.NewServer(mux.Handler())
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodOptions, srv.URL+"/v1/projects", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", console)
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if authenticated {
		t.Error("authentication ran on a preflight; it would answer 401 and the real request would never be sent")
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != console {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, console)
	}
}

func slicesContainsFold(hay []string, needle string) bool {
	for _, v := range hay {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), needle) {
				return true
			}
		}
	}
	return false
}
