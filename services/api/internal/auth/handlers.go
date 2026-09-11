package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/chassis/httpx"
	"github.com/anasatwork01/cofound/packages/db"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/apiv1"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/common"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// challengeCookie holds the OAuth state and PKCE verifier between the redirect
// to Google and the callback.
//
// A cookie rather than a server-side row: it is single-use, lives for one
// redirect, and belongs to exactly one browser. Storing it in Postgres would
// add a table and a sweeper to hold something the browser can hold itself, and
// the cookie is httpOnly so page scripts cannot read it.
const challengeCookie = "halyard_oauth_challenge"

// challengeTTL bounds how long a started sign-in can be completed.
//
// Ten minutes. Long enough for a consent screen and a password manager, short
// enough that an abandoned attempt is not a credential sitting in a browser
// for a day.
const challengeTTL = 10 * time.Minute

// Handlers is the /v1/auth surface.
type Handlers struct {
	Sessions *Sessions
	Links    *MagicLinks
	Google   *Google
	Pool     *db.Pool
	Cookies  CookieConfig
	Audit    AuditWriter

	// ConsoleOrigin is where the OAuth callback sends the browser afterwards.
	// From configuration, never from the request.
	ConsoleOrigin string

	// pending holds a rotated token between UserFor and Rotated, keyed by the
	// request.
	//
	// A map keyed by *http.Request rather than a context value, because the
	// tenancy middleware calls UserFor with the request it was given and cannot
	// hand a new context back up to whoever wrote the response. Entries are
	// removed by LoadAndDelete on the same request, so nothing accumulates —
	// and a request whose handler never calls Rotated simply does not rotate,
	// which is safe: the old token is still inside its grace window.
	pending sync.Map
}

// AuditWriter records the events SPEC §8 requires an audit trail for.
//
// An interface so that task 0.9, which owns the audit_log writer and the full
// nine-action list, can supply the real one without this package depending on
// it. Sign-in is the first of those nine and happens here, so the seam exists
// now rather than being retrofitted.
type AuditWriter interface {
	SignIn(ctx context.Context, userID uuid.UUID, method string, ip net.IP, userAgent string) error
}

// Mount registers the auth routes on the public subtree.
//
// Public, including "who am I" and sign-out. These are the endpoints that
// ESTABLISH who the caller is, so they cannot sit behind middleware that needs
// that answer already — /auth/session reads the cookie itself and returns 401
// when there is none.
func (h *Handlers) Mount(r chi.Router, ew *httpx.ErrorWriter) {
	r.Route("/v1/auth", func(r chi.Router) {
		r.Post("/magic-link", ew.H(h.requestMagicLink))
		r.Post("/magic-link/verify", ew.H(h.verifyMagicLink))
		r.Get("/google/start", ew.H(h.startGoogle))
		r.Get("/google/callback", ew.H(h.completeGoogle))
		r.Get("/session", ew.H(h.getSession))
		r.Delete("/session", ew.H(h.signOut))
	})
}

// maxAuthBody bounds an auth request body.
//
// These bodies are one short field. 4KB is generous for that and refuses a
// caller who wants us to parse a megabyte before finding out it is invalid.
const maxAuthBody = 4 << 10

func (h *Handlers) requestMagicLink(w http.ResponseWriter, r *http.Request) error {
	body, err := httpx.Decode[struct {
		Email string `json:"email"`
	}](r, maxAuthBody)
	if err != nil {
		return err
	}

	switch err := h.Links.Request(r.Context(), body.Email, clientIP(r), r.UserAgent()); {
	case err == nil:
	case errors.Is(err, ErrThrottled):
		// Retry-After from the throttle window, so the console can say when
		// rather than "try again later".
		return errs.RateLimited(h.Links.policy.Window).
			WithMessage("Too many sign-in links have been requested for that address.").
			WithFix("Check your inbox, or wait a few minutes and try again.")
	case errs.CodeOf(err) == errs.CodeInvalid:
		// A malformed address is worth reporting: the user typed it, and
		// silently accepting it would leave them waiting for an email that can
		// never arrive.
		return err
	default:
		return err
	}

	// 202 unconditionally, whether or not the address has an account. A
	// different response for a known address turns this endpoint into a way to
	// test whether a given person has one.
	w.WriteHeader(http.StatusAccepted)
	return nil
}

func (h *Handlers) verifyMagicLink(w http.ResponseWriter, r *http.Request) error {
	body, err := httpx.Decode[struct {
		Token string `json:"token"`
	}](r, maxAuthBody)
	if err != nil {
		return err
	}
	token, ok := ParseToken(body.Token)
	if !ok {
		return Unauthenticated()
	}

	userID, _, err := h.Links.Consume(r.Context(), token)
	if err != nil {
		if errors.Is(err, ErrNoSession) {
			return Unauthenticated().
				WithMessage("That sign-in link is no longer valid.").
				WithFix("Request a new one.")
		}
		return err
	}
	return h.signIn(w, r, userID, "magic_link")
}

func (h *Handlers) startGoogle(w http.ResponseWriter, r *http.Request) error {
	challenge, err := NewChallenge()
	if err != nil {
		return err
	}
	target, err := h.Google.AuthorizeURL(challenge)
	if err != nil {
		return err
	}

	// state and verifier travel together in one httpOnly cookie. SameSite=Lax,
	// because the callback arrives as a cross-site top-level navigation FROM
	// Google — Strict would withhold the cookie on exactly the request that
	// needs it, and the flow would fail every time.
	http.SetCookie(w, &http.Cookie{
		Name:     challengeCookie,
		Value:    challenge.State + "." + challenge.Verifier,
		Path:     "/v1/auth/google",
		HttpOnly: true,
		Secure:   h.Cookies.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(challengeTTL.Seconds()),
	})
	http.Redirect(w, r, target, http.StatusSeeOther)
	return nil
}

func (h *Handlers) completeGoogle(w http.ResponseWriter, r *http.Request) error {
	// Clear the challenge before doing anything else, so a failed attempt
	// cannot be retried with the same state.
	defer http.SetCookie(w, &http.Cookie{
		Name: challengeCookie, Value: "", Path: "/v1/auth/google",
		HttpOnly: true, Secure: h.Cookies.Secure, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})

	q := r.URL.Query()
	if reason := q.Get("error"); reason != "" {
		// The user declined, or Google refused. Not an error on our side, and
		// the browser is mid-navigation, so send them back to the console
		// rather than rendering JSON at a human.
		return h.redirectToConsole(w, r, "declined")
	}

	challenge, ok := h.readChallenge(r)
	if !ok {
		return h.redirectToConsole(w, r, "expired")
	}
	// Constant-time, and the comparison that makes `state` worth having: it is
	// what stops an attacker completing the flow in a victim's browser using a
	// code they obtained themselves.
	if !Hash(challenge.State).Equal(Hash(q.Get("state"))) {
		return h.redirectToConsole(w, r, "state_mismatch")
	}

	identity, err := h.Google.Exchange(r.Context(), q.Get("code"), challenge)
	if err != nil {
		// The cause carries the provider's response, which can echo our client
		// secret, so it reaches the log and never the browser.
		return h.redirectToConsole(w, r, "exchange_failed")
	}
	userID, err := h.Google.Upsert(r.Context(), identity, clientIP(r))
	if err != nil {
		if errs.CodeOf(err) == errs.CodeForbidden {
			return h.redirectToConsole(w, r, "email_unverified")
		}
		return err
	}

	token, _, err := h.Sessions.Issue(r.Context(), userID, clientIP(r), r.UserAgent())
	if err != nil {
		return err
	}
	h.Cookies.Set(w, token)
	h.recordSignIn(r.Context(), userID, "google", r)
	return h.redirectToConsole(w, r, "")
}

// redirectToConsole sends the browser back, with a reason the console can show.
//
// The reason is a stable code, never a message: the console owns the wording
// (SPEC §18), and a reason built from an upstream error string would put
// provider internals on a page.
func (h *Handlers) redirectToConsole(w http.ResponseWriter, r *http.Request, reason string) error {
	target, err := url.Parse(h.ConsoleOrigin)
	if err != nil || target.Host == "" {
		return errs.Internal(fmt.Errorf("auth: CONSOLE_ORIGIN is not a usable URL"))
	}
	target.Path = "/auth/callback"
	if reason != "" {
		target.RawQuery = url.Values{"error": {reason}}.Encode()
	}
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
	return nil
}

func (h *Handlers) readChallenge(r *http.Request) (Challenge, bool) {
	ck, err := r.Cookie(challengeCookie)
	if err != nil {
		return Challenge{}, false
	}
	state, verifier, found := cut(ck.Value, ".")
	if !found || state == "" || verifier == "" {
		return Challenge{}, false
	}
	return Challenge{State: state, Verifier: verifier}, true
}

func (h *Handlers) getSession(w http.ResponseWriter, r *http.Request) error {
	token, ok := h.Cookies.Read(r)
	if !ok {
		return Unauthenticated()
	}
	sess, rotated, err := h.Sessions.Verify(r.Context(), token, clientIP(r), r.UserAgent())
	if err != nil {
		if errors.Is(err, ErrNoSession) {
			// Clear the cookie the browser is holding. Without this the console
			// keeps sending a dead token on every request forever.
			h.Cookies.Clear(w)
			return Unauthenticated()
		}
		return err
	}
	// Rotation must be honoured on EVERY response, not only on sign-in, or it
	// silently never takes effect.
	if !rotated.IsZero() {
		h.Cookies.Set(w, rotated)
	}

	view, err := h.view(r.Context(), sess.UserID)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, view)
}

func (h *Handlers) signOut(w http.ResponseWriter, r *http.Request) error {
	if token, ok := h.Cookies.Read(r); ok {
		if sess, _, err := h.Sessions.Verify(r.Context(), token, clientIP(r), r.UserAgent()); err == nil {
			if err := h.Sessions.Revoke(r.Context(), sess.ID, "signed out"); err != nil {
				return err
			}
		}
	}
	// Always clear and always 204. Signing out when already signed out is not
	// an error, and reporting one would leave a console with a dead cookie and
	// a red banner.
	h.Cookies.Clear(w)
	return httpx.NoContent(w)
}

// signIn issues the session and answers with the viewer.
func (h *Handlers) signIn(w http.ResponseWriter, r *http.Request, userID uuid.UUID, method string) error {
	token, _, err := h.Sessions.Issue(r.Context(), userID, clientIP(r), r.UserAgent())
	if err != nil {
		return err
	}
	h.Cookies.Set(w, token)
	h.recordSignIn(r.Context(), userID, method, r)

	view, err := h.view(r.Context(), userID)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, view)
}

// recordSignIn writes the §8 audit entry.
//
// Best-effort, and that is a decision rather than an oversight: a user who has
// just proven their identity should not be refused a session because the audit
// insert failed. The failure is logged, and task 0.9 — which owns the audit
// writer — is where a durable queue for it belongs if one is wanted.
func (h *Handlers) recordSignIn(ctx context.Context, userID uuid.UUID, method string, r *http.Request) {
	if h.Audit == nil {
		return
	}
	_ = h.Audit.SignIn(ctx, userID, method, clientIP(r), r.UserAgent())
}

// view builds the AuthSession response: the user and every org they belong to.
//
// Read UNSCOPED, deliberately. This is the query that establishes which orgs
// the caller may scope to, so it cannot itself run inside an org scope — that
// would be circular. It is also why the org list is filtered by user_id here
// rather than relying on a policy.
func (h *Handlers) view(ctx context.Context, userID uuid.UUID) (apiv1.AuthSession, error) {
	var out apiv1.AuthSession

	var email string
	var name *string
	if err := h.Pool.Unscoped().QueryRow(ctx,
		`select email, name from users where id = $1`, userID).Scan(&email, &name); err != nil {
		return out, fmt.Errorf("auth: load user: %w", err)
	}
	out.User.Id = common.Uuid(userID.String())
	out.User.Email = openapi_types.Email(email)
	out.User.Name = name

	rows, err := h.Pool.Unscoped().Query(ctx, `
		select o.id, o.slug, o.name, m.role
		  from org_members m
		  join orgs o on o.id = m.org_id
		 where m.user_id = $1
		 order by o.name`, userID)
	if err != nil {
		return out, fmt.Errorf("auth: load orgs: %w", err)
	}
	defer rows.Close()

	// Non-nil even when empty: the schema marks `orgs` required, and a nil
	// slice marshals to null rather than [], which the generated decoder on the
	// console side would reject.
	out.Orgs = []apiv1.AuthOrgMembership{}
	for rows.Next() {
		var m apiv1.AuthOrgMembership
		var id, slug, orgName, role string
		if err := rows.Scan(&id, &slug, &orgName, &role); err != nil {
			return out, fmt.Errorf("auth: scan org: %w", err)
		}
		m.Id, m.Slug, m.Name, m.Role = common.Uuid(id), common.Slug(slug), orgName, common.Role(role)
		out.Orgs = append(out.Orgs, m)
	}
	return out, rows.Err()
}

// clientIP reads the peer the chassis resolved.
//
// The chassis's ClientIP middleware already applied the TRUSTED_PROXY_CIDRS
// policy, so this does not re-parse X-Forwarded-For: doing so would trust a
// header the chassis deliberately does not.
func clientIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

func cut(s, sep string) (before, after string, found bool) {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return s[:i], s[i+len(sep):], true
		}
	}
	return s, "", false
}

// UserFor implements tenancy.Authenticator.
//
// Returns the signed-in user, or a sentinel the tenancy middleware maps to 401.
// It deliberately does NOT write a rotated cookie: a resolution that then fails
// with 404 must not leave the browser holding a token inside its grace window,
// so rotation is applied by Rotated at a point the caller chooses.
func (h *Handlers) UserFor(r *http.Request) (uuid.UUID, error) {
	token, ok := h.Cookies.Read(r)
	if !ok {
		return uuid.Nil, ErrNoSession
	}
	sess, rotated, err := h.Sessions.Verify(r.Context(), token, clientIP(r), r.UserAgent())
	if err != nil {
		return uuid.Nil, err
	}
	if !rotated.IsZero() {
		// Stashed rather than written, so Rotated can flush it once the request
		// is known to be going ahead.
		h.pending.Store(r, rotated)
	}
	return sess.UserID, nil
}

// Rotated writes a refreshed session cookie if Verify issued one.
//
// Called by the tenancy middleware immediately after authentication succeeds.
// Rotation has to be honoured on EVERY response, not only on sign-in, or it
// silently never takes effect — and the browser then keeps presenting a token
// that stops working when its grace window closes.
func (h *Handlers) Rotated(w http.ResponseWriter, r *http.Request) {
	if token, ok := h.pending.LoadAndDelete(r); ok {
		if t, fine := token.(Token); fine && !t.IsZero() {
			h.Cookies.Set(w, t)
		}
	}
}
