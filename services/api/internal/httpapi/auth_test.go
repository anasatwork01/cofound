package httpapi_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/anasatwork01/cofound/packages/chassis"
	"github.com/anasatwork01/cofound/packages/chassis/config"
	"github.com/anasatwork01/cofound/packages/chassis/logging"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/apiv1"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/common"
	"github.com/anasatwork01/cofound/services/api/internal/auth"
	"github.com/anasatwork01/cofound/services/api/internal/httpapi"
	"github.com/google/uuid"
)

// captureMailer stands in for the transactional email provider nobody has
// chosen yet (docs/open-questions.md Q5) and lets the test read the link.
type captureMailer struct {
	mu    sync.Mutex
	links map[string]string
}

func (m *captureMailer) SendMagicLink(_ context.Context, email, link string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.links == nil {
		m.links = map[string]string{}
	}
	m.links[email] = link
	return nil
}

func (m *captureMailer) link(email string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.links[email]
}

// authHarness boots the real service against the real database as the
// unprivileged role, because sign-in writes rows and the whole point of the
// session store is that it is durable.
type authHarness struct {
	base   string
	mail   *captureMailer
	sink   *logging.Sink
	client *http.Client
	pool   interface{ Close() }
}

func bootAuth(t *testing.T) *authHarness {
	t.Helper()
	appURL := os.Getenv("APP_DATABASE_URL")
	if appURL == "" {
		if os.Getenv("DATABASE_URL") != "" {
			t.Fatal("APP_DATABASE_URL is unset but DATABASE_URL is set; the harness is misconfigured")
		}
		t.Skip("APP_DATABASE_URL unset; run `make db-setup`")
	}

	mail := &captureMailer{}
	sink := logging.NewSink()
	api := httpapi.New()
	api.MailerFor = func(*chassis.Runtime) auth.Mailer { return mail }

	c, err := chassis.New(context.Background(), chassis.Options{
		Service: chassis.Service{
			Name: "api", Version: "test", Commit: "test",
			Bind: api.Bind, Setup: api.Setup, Probes: api.Probes,
		},
		Lookup: config.MapLookup(map[string]string{
			"HALYARD_ENV":    "development",
			"DATABASE_URL":   appURL,
			"REDIS_URL":      "redis://localhost:56379/0",
			"CONSOLE_ORIGIN": "http://console.test",
		}),
		Out: sink,
	})
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	t.Cleanup(func() {
		if api.DB != nil {
			api.DB.Close()
		}
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: c.Handler()}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	jar := &cookieJar{}
	return &authHarness{
		base: "http://" + ln.Addr().String(),
		mail: mail,
		sink: sink,
		client: &http.Client{
			Jar: jar,
			// Do not follow redirects: the OAuth tests assert on the Location.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// cookieJar is a minimal jar. net/http/cookiejar refuses to store cookies for
// an IP-address host, which every test server has.
type cookieJar struct {
	mu sync.Mutex
	ck map[string]*http.Cookie
}

func (j *cookieJar) SetCookies(_ *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.ck == nil {
		j.ck = map[string]*http.Cookie{}
	}
	for _, c := range cookies {
		if c.MaxAge < 0 {
			delete(j.ck, c.Name)
			continue
		}
		j.ck[c.Name] = c
	}
}

func (j *cookieJar) Cookies(*url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]*http.Cookie, 0, len(j.ck))
	for _, c := range j.ck {
		out = append(out, c)
	}
	return out
}

func (h *authHarness) post(t *testing.T, path string, body any) (*http.Response, string) {
	t.Helper()
	var payload strings.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		payload = *strings.NewReader(string(b))
	}
	req, err := http.NewRequest(http.MethodPost, h.base+path, &payload)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	return h.do(t, req)
}

func (h *authHarness) req(t *testing.T, method, path string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, h.base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return h.do(t, req)
}

func (h *authHarness) do(t *testing.T, req *http.Request) (*http.Response, string) {
	t.Helper()
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	buf := make([]byte, 1<<16)
	n, _ := resp.Body.Read(buf)
	return resp, string(buf[:n])
}

func testEmail() string { return "flow-" + uuid.NewString()[:8] + "@test.invalid" }

// tokenFrom pulls the token out of the captured magic link.
func tokenFrom(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("the magic link is not a URL: %q", link)
	}
	tok := u.Query().Get("token")
	if tok == "" {
		t.Fatalf("the magic link carries no token: %q", link)
	}
	return tok
}

// TestMagicLinkSignsAUserIn is the whole flow, end to end.
func TestMagicLinkSignsAUserIn(t *testing.T) {
	t.Parallel()
	h := bootAuth(t)
	email := testEmail()

	resp, _ := h.post(t, "/v1/auth/magic-link", map[string]string{"email": email})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("magic-link = %d, want 202", resp.StatusCode)
	}

	link := h.mail.link(email)
	if link == "" {
		t.Fatal("no link was sent")
	}
	// Built from CONSOLE_ORIGIN, never from the request's Host: otherwise an
	// attacker who can set Host has us email a link pointing at their server.
	if !strings.HasPrefix(link, "http://console.test/auth/callback") {
		t.Errorf("the link does not point at CONSOLE_ORIGIN: %s", link)
	}

	resp, body := h.post(t, "/v1/auth/magic-link/verify",
		map[string]string{"token": tokenFrom(t, link)})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify = %d, want 200: %s", resp.StatusCode, body)
	}

	var view apiv1.AuthSession
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("the generated decoder rejected the response: %v\n%s", err, body)
	}
	if string(view.User.Email) != email {
		t.Errorf("signed in as %q, want %q", view.User.Email, email)
	}
	// A brand new user belongs to no org, and the field must still be [] rather
	// than null — the schema marks it required and null fails the decoder.
	if view.Orgs == nil {
		t.Error("orgs is null; it must be an empty array")
	}
	if !strings.Contains(body, `"orgs":[]`) {
		t.Errorf("orgs should serialise as []: %s", body)
	}

	// The session cookie now works on its own.
	resp, body = h.req(t, http.MethodGet, "/v1/auth/session")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session = %d, want 200: %s", resp.StatusCode, body)
	}
}

// TestTheSessionCookieHasTheAttributesSpecEightRequires.
func TestTheSessionCookieHasTheAttributesSpecEightRequires(t *testing.T) {
	t.Parallel()
	h := bootAuth(t)
	email := testEmail()
	h.post(t, "/v1/auth/magic-link", map[string]string{"email": email})
	resp, _ := h.post(t, "/v1/auth/magic-link/verify",
		map[string]string{"token": tokenFrom(t, h.mail.link(email))})

	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if strings.Contains(c.Name, "halyard_session") {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no session cookie was set")
	}
	if !session.HttpOnly {
		t.Error("the cookie is not HttpOnly; page scripts could read the session")
	}
	if session.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax (SPEC §8)", session.SameSite)
	}
	if session.Path != "/" {
		t.Errorf("Path = %q, want /", session.Path)
	}
	// No Domain, so the cookie is never sent to a preview subdomain running
	// code the agent wrote (SPEC §17).
	if session.Domain != "" {
		t.Errorf("Domain = %q; the cookie must not be shared with subdomains", session.Domain)
	}
	// Development runs over HTTP, so Secure is off and the __Host- prefix — which
	// a browser only accepts WITH Secure — must not be used.
	if session.Secure {
		t.Error("Secure is set in a development boot served over HTTP")
	}
	if strings.HasPrefix(session.Name, "__Host-") {
		t.Error("a __Host- cookie without Secure is silently dropped by browsers")
	}
}

// TestAMagicLinkWorksExactlyOnce.
func TestAMagicLinkWorksExactlyOnce(t *testing.T) {
	t.Parallel()
	h := bootAuth(t)
	email := testEmail()
	h.post(t, "/v1/auth/magic-link", map[string]string{"email": email})
	token := tokenFrom(t, h.mail.link(email))

	if resp, body := h.post(t, "/v1/auth/magic-link/verify",
		map[string]string{"token": token}); resp.StatusCode != http.StatusOK {
		t.Fatalf("first use = %d: %s", resp.StatusCode, body)
	}
	resp, body := h.post(t, "/v1/auth/magic-link/verify", map[string]string{"token": token})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("second use = %d, want 401: %s", resp.StatusCode, body)
	}
}

// TestVerifyRevealsNothingAboutWhichWayItFailed.
//
// Unknown, spent and expired tokens must be indistinguishable, or the endpoint
// becomes an oracle for whether a captured token was ever real.
func TestVerifyRevealsNothingAboutWhichWayItFailed(t *testing.T) {
	t.Parallel()
	h := bootAuth(t)
	email := testEmail()
	h.post(t, "/v1/auth/magic-link", map[string]string{"email": email})
	spent := tokenFrom(t, h.mail.link(email))
	h.post(t, "/v1/auth/magic-link/verify", map[string]string{"token": spent})

	fresh, _, err := auth.NewToken()
	if err != nil {
		t.Fatal(err)
	}

	var bodies []string
	for _, tok := range []string{spent, fresh.Secret()} {
		resp, body := h.post(t, "/v1/auth/magic-link/verify", map[string]string{"token": tok})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
		var env common.ErrorResponse
		if err := json.Unmarshal([]byte(body), &env); err != nil {
			t.Fatalf("not an envelope: %v", err)
		}
		bodies = append(bodies, env.Error.Code+"|"+env.Error.Message)
	}
	if bodies[0] != bodies[1] {
		t.Errorf("a spent token and an unknown one are distinguishable:\n  %s\n  %s",
			bodies[0], bodies[1])
	}
}

// TestRequestingALinkNeverRevealsWhetherAnAccountExists.
func TestRequestingALinkNeverRevealsWhetherAnAccountExists(t *testing.T) {
	t.Parallel()
	h := bootAuth(t)

	known := testEmail()
	h.post(t, "/v1/auth/magic-link", map[string]string{"email": known})
	h.post(t, "/v1/auth/magic-link/verify",
		map[string]string{"token": tokenFrom(t, h.mail.link(known))})

	var seen []int
	for _, email := range []string{known, testEmail()} {
		resp, _ := h.post(t, "/v1/auth/magic-link", map[string]string{"email": email})
		seen = append(seen, resp.StatusCode)
	}
	if seen[0] != seen[1] {
		t.Errorf("a known address answers %d and an unknown one %d; that is an account oracle",
			seen[0], seen[1])
	}
}

func TestMagicLinkRejectsAMalformedAddress(t *testing.T) {
	t.Parallel()
	h := bootAuth(t)
	// The user typed this, so silently accepting it would leave them waiting
	// for an email that can never arrive.
	resp, body := h.post(t, "/v1/auth/magic-link", map[string]string{"email": "not-an-email"})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", resp.StatusCode, body)
	}
	var env common.ErrorResponse
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("not an envelope: %v", err)
	}
	if env.Error.Code != "invalid" {
		t.Errorf("code = %q", env.Error.Code)
	}
}

func TestMagicLinkThrottlesOneAddress(t *testing.T) {
	t.Parallel()
	h := bootAuth(t)
	email := testEmail()

	// The policy is five per hour. The sixth must be refused: without this the
	// endpoint is a free email-bombing service pointed at someone else's inbox.
	var last *http.Response
	for range 6 {
		last, _ = h.post(t, "/v1/auth/magic-link", map[string]string{"email": email})
	}
	if last.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("the sixth request = %d, want 429", last.StatusCode)
	}
	if last.Header.Get("Retry-After") == "" {
		t.Error("a 429 must carry Retry-After, or a client can only guess")
	}
}

func TestSignOutRevokesAndIsIdempotent(t *testing.T) {
	t.Parallel()
	h := bootAuth(t)
	email := testEmail()
	h.post(t, "/v1/auth/magic-link", map[string]string{"email": email})
	h.post(t, "/v1/auth/magic-link/verify",
		map[string]string{"token": tokenFrom(t, h.mail.link(email))})

	if resp, _ := h.req(t, http.MethodGet, "/v1/auth/session"); resp.StatusCode != http.StatusOK {
		t.Fatal("not signed in before sign-out")
	}
	if resp, _ := h.req(t, http.MethodDelete, "/v1/auth/session"); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("sign-out = %d, want 204", resp.StatusCode)
	}
	if resp, _ := h.req(t, http.MethodGet, "/v1/auth/session"); resp.StatusCode != http.StatusUnauthorized {
		t.Error("the session still works after sign-out")
	}
	// Idempotent: signing out twice is not an error, or a console ends up with
	// a dead cookie and a red banner.
	if resp, _ := h.req(t, http.MethodDelete, "/v1/auth/session"); resp.StatusCode != http.StatusNoContent {
		t.Error("signing out twice should still be 204")
	}
}

func TestSessionIsUnauthenticatedWithoutACookie(t *testing.T) {
	t.Parallel()
	h := bootAuth(t)
	resp, body := h.req(t, http.MethodGet, "/v1/auth/session")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	var env common.ErrorResponse
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("not an envelope: %v", err)
	}
	if env.Error.Code != "unauthenticated" {
		t.Errorf("code = %q", env.Error.Code)
	}
}

// TestGoogleIsUnavailableWhenUnconfigured.
//
// A deployment without Google credentials must degrade to magic links, not to
// a redirect that fails at the consent screen.
func TestGoogleIsUnavailableWhenUnconfigured(t *testing.T) {
	t.Parallel()
	h := bootAuth(t)
	resp, _ := h.req(t, http.MethodGet, "/v1/auth/google/start")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when Google is not configured", resp.StatusCode)
	}
}

// TestTheMagicLinkNeverReachesTheLog.
//
// The link carries a live single-use credential. LogMailer marks it as user
// content, so the chassis redactor replaces it above debug level — which is
// what stops a staging box spraying sign-in links into a log pipeline.
func TestTheMagicLinkNeverReachesTheLog(t *testing.T) {
	t.Parallel()
	h := bootAuth(t)
	email := testEmail()
	h.post(t, "/v1/auth/magic-link", map[string]string{"email": email})
	token := tokenFrom(t, h.mail.link(email))
	h.post(t, "/v1/auth/magic-link/verify", map[string]string{"token": token})

	if h.sink.Contains(token) {
		t.Error("a live magic-link token reached the log")
	}
}
