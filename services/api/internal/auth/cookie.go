package auth

import (
	"net/http"
	"time"
)

// CookieName is the session cookie.
//
// The `__Host-` prefix is not decoration. A browser will only accept such a
// cookie if it is Secure, has Path=/, and has NO Domain attribute — which means
// it cannot be set by, or sent to, a sibling subdomain. That matters here
// because SPEC §9 gives every preview sandbox a hostname under the platform's
// own zone: without the prefix, a cookie scoped to `.halyard.dev` would be sent
// to code the agent wrote, and the agent's code is untrusted (§17).
//
// The cost is that it cannot be used over plain HTTP, so local development
// falls back to the unprefixed name. That fallback is chosen by Secure below,
// not by a separate switch, so the two cannot disagree.
const (
	CookieName         = "__Host-halyard_session"
	insecureCookieName = "halyard_session"
)

// CookieConfig shapes the session cookie.
type CookieConfig struct {
	// Secure is off only for local HTTP development. It also selects the cookie
	// NAME, because __Host- requires Secure and a browser silently drops a
	// __Host- cookie that lacks it — which would present as "sign-in does
	// nothing", with no error anywhere.
	Secure bool

	// MaxAge is the browser-side lifetime. It tracks the session's absolute
	// deadline rather than its idle one: a cookie that expired in the browser
	// while the server-side session was still valid would sign the user out
	// mid-task, and a cookie that outlives the session is harmless because the
	// server rejects it.
	MaxAge time.Duration
}

// Name returns the cookie name this configuration can actually set.
func (c CookieConfig) Name() string {
	if c.Secure {
		return CookieName
	}
	return insecureCookieName
}

// Set writes the session cookie.
//
// SameSite=Lax, per §8. Not Strict: a magic link arrives from an email client
// as a cross-site top-level navigation, and Strict would withhold the cookie on
// exactly that request — so the user would click the link, land signed out, and
// try again. Lax sends cookies on top-level GET navigations, which is that case
// and not much else. Not None either: None permits the cookie on cross-site
// POSTs, which is the CSRF surface Lax exists to close.
func (c CookieConfig) Set(w http.ResponseWriter, token Token) {
	http.SetCookie(w, &http.Cookie{
		Name:     c.Name(),
		Value:    token.Secret(),
		Path:     "/",
		HttpOnly: true,
		Secure:   c.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(c.MaxAge.Seconds()),
		// No Domain, deliberately. __Host- forbids it, and even unprefixed the
		// cookie must not be shared with a preview subdomain.
	})
}

// Clear expires the session cookie.
//
// Attributes must match what Set wrote or the browser treats it as a different
// cookie and leaves the original in place — a sign-out that appears to work and
// does not.
func (c CookieConfig) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     c.Name(),
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   c.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// Read extracts the session token from a request.
//
// Accepts either name regardless of configuration, so that a deployment moving
// from HTTP to HTTPS does not sign every user out at the moment the cookie name
// changes. The token is validated against the database either way, so reading a
// name we would not now set costs nothing.
func (c CookieConfig) Read(r *http.Request) (Token, bool) {
	for _, name := range []string{CookieName, insecureCookieName} {
		if ck, err := r.Cookie(name); err == nil {
			if t, ok := ParseToken(ck.Value); ok {
				return t, true
			}
		}
	}
	return Token{}, false
}
