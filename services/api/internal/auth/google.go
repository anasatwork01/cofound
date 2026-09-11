package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/db"
	"github.com/google/uuid"
)

// Google endpoint defaults.
//
// Configurable rather than constant, and read from configuration at startup, so
// that a change on Google's side is a deploy rather than a release. They are
// Google's published OpenID Connect endpoints; the discovery document at
// https://accounts.google.com/.well-known/openid-configuration is the
// authoritative source and is what to check against if sign-in starts failing.
const (
	defaultGoogleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	defaultGoogleTokenURL = "https://oauth2.googleapis.com/token"
	googleIssuer          = "https://accounts.google.com"
)

// GoogleConfig is the OAuth client.
type GoogleConfig struct {
	ClientID     string
	ClientSecret string
	// RedirectURL must match a URI registered on the Google client exactly.
	// Built from configuration rather than from the request, for the same
	// reason the magic link is: a Host header an attacker controls must not be
	// able to redirect the flow at their own server.
	RedirectURL string

	AuthURL  string
	TokenURL string
}

// Google implements the authorization-code flow with PKCE.
type Google struct {
	cfg    GoogleConfig
	pool   *db.Pool
	client *http.Client
	now    func() time.Time
}

// NewGoogle returns the flow. A nil client means a bounded default.
func NewGoogle(pool *db.Pool, cfg GoogleConfig, client *http.Client, now func() time.Time) *Google {
	if cfg.AuthURL == "" {
		cfg.AuthURL = defaultGoogleAuthURL
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = defaultGoogleTokenURL
	}
	if now == nil {
		now = time.Now
	}
	if client == nil {
		// A timeout, because this call sits on a user-facing request path and
		// http.DefaultClient has none: a hung token endpoint would hold the
		// handler until the chassis's own deadline fired.
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Google{cfg: cfg, pool: pool, client: client, now: now}
}

// Configured reports whether Google sign-in is available.
//
// Checked rather than assumed so the console can hide the button, and so a
// deployment without Google credentials serves magic links instead of a broken
// redirect.
func (g *Google) Configured() bool {
	return g.cfg.ClientID != "" && g.cfg.ClientSecret != "" && g.cfg.RedirectURL != ""
}

// Challenge is the per-attempt state a caller must carry in a cookie.
type Challenge struct {
	State    string
	Verifier string
}

// NewChallenge creates the CSRF state and the PKCE verifier.
//
// Both, not one. They defend different things and neither substitutes for the
// other: `state` stops an attacker completing the flow in the victim's browser
// with their own code, and the PKCE verifier stops an intercepted code being
// redeemed by anyone who did not start the flow. PKCE is often described as
// optional for confidential clients; it costs two fields here and removes the
// authorization-code interception class entirely, so there is no reason to
// reason about whether we are safe without it.
func NewChallenge() (Challenge, error) {
	state, _, err := NewToken()
	if err != nil {
		return Challenge{}, err
	}
	verifier, _, err := NewToken()
	if err != nil {
		return Challenge{}, err
	}
	return Challenge{State: state.Secret(), Verifier: verifier.Secret()}, nil
}

// AuthorizeURL is where the browser is sent.
func (g *Google) AuthorizeURL(c Challenge) (string, error) {
	if !g.Configured() {
		return "", errs.Chassis().New(errs.CodeNotFound).
			WithMessage("Google sign-in is not available.").
			WithFix("Sign in with an email link instead.")
	}
	u, err := url.Parse(g.cfg.AuthURL)
	if err != nil {
		return "", fmt.Errorf("auth: google auth url: %w", err)
	}
	u.RawQuery = url.Values{
		"client_id":     {g.cfg.ClientID},
		"redirect_uri":  {g.cfg.RedirectURL},
		"response_type": {"code"},
		// openid and email only. Not profile, not anything else: §17 and the
		// principle behind it say take the narrowest scope that works, and all
		// this flow needs is a stable subject and an address. A broader scope
		// is a consent screen that asks for more than we use.
		"scope": {"openid email"},
		"state": {c.State},
		// S256, never plain. `plain` sends the verifier itself, which defeats
		// the mechanism.
		"code_challenge":        {pkceChallenge(c.Verifier)},
		"code_challenge_method": {"S256"},
	}.Encode()
	return u.String(), nil
}

func pkceChallenge(verifier string) string {
	sum := sha256sum([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum)
}

// GoogleIdentity is what the provider asserted.
type GoogleIdentity struct {
	Subject string
	Email   string
	// Verified is Google's own claim about the address. An unverified address
	// must not be trusted to identify an account: with a Workspace domain an
	// administrator can create an unverified alias for anyone, and honouring it
	// would let them take over that person's account here.
	Verified bool
}

// Exchange redeems the code and returns the identity it proves.
func (g *Google) Exchange(ctx context.Context, code string, c Challenge) (GoogleIdentity, error) {
	if !g.Configured() {
		return GoogleIdentity{}, errors.New("auth: google is not configured")
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {g.cfg.ClientID},
		"client_secret": {g.cfg.ClientSecret},
		"redirect_uri":  {g.cfg.RedirectURL},
		"code_verifier": {c.Verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.TokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return GoogleIdentity{}, fmt.Errorf("auth: token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return GoogleIdentity{}, fmt.Errorf("auth: token endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Bounded read. A compromised or misbehaving token endpoint should not be
	// able to make us allocate without limit.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return GoogleIdentity{}, fmt.Errorf("auth: token body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// The provider's error body can echo the code and the client secret, so
		// it reaches the log through the wrapped cause and never the response.
		return GoogleIdentity{}, fmt.Errorf("auth: token endpoint returned %d", resp.StatusCode)
	}

	var out struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return GoogleIdentity{}, fmt.Errorf("auth: token response: %w", err)
	}
	if out.IDToken == "" {
		return GoogleIdentity{}, errors.New("auth: token response carried no id_token")
	}
	return parseIDToken(out.IDToken, g.cfg.ClientID, g.now().UTC())
}

// parseIDToken reads the claims without verifying the signature.
//
// That is deliberate and it is standards-sanctioned, not a shortcut. OpenID
// Connect Core §3.1.3.7 item 6: "If the ID Token is received via direct
// communication between the Client and the Token Endpoint, the TLS server
// validation MAY be used to validate the issuer in place of checking the token
// signature." This token arrived on our own TLS connection to Google's token
// endpoint, authenticated with our client secret — not via the browser — so the
// transport already establishes who sent it.
//
// The alternative is fetching and caching Google's JWKS, rotating it, and
// handling the failure modes of a second network dependency on the sign-in
// path. That is worth doing for a token arriving through an untrusted channel;
// here it would add moving parts without adding a property.
//
// The claims that are NOT optional are checked below: issuer, audience and
// expiry. Skipping the signature is only sound while those are enforced.
func parseIDToken(raw, clientID string, now time.Time) (GoogleIdentity, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return GoogleIdentity{}, errors.New("auth: id_token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return GoogleIdentity{}, fmt.Errorf("auth: id_token payload: %w", err)
	}
	var claims struct {
		Issuer   string `json:"iss"`
		Audience string `json:"aud"`
		Subject  string `json:"sub"`
		Email    string `json:"email"`
		Verified any    `json:"email_verified"`
		Expiry   int64  `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return GoogleIdentity{}, fmt.Errorf("auth: id_token claims: %w", err)
	}

	// Issuer. Google emits both spellings historically, so both are accepted
	// and nothing else is.
	if claims.Issuer != googleIssuer && claims.Issuer != "accounts.google.com" {
		return GoogleIdentity{}, fmt.Errorf("auth: id_token issuer %q is not Google", claims.Issuer)
	}
	// Audience. Without this check a token minted for a DIFFERENT Google client
	// — anyone's — would be accepted here, which is the whole confused-deputy
	// problem the claim exists to prevent.
	if claims.Audience != clientID {
		return GoogleIdentity{}, errors.New("auth: id_token was issued for another client")
	}
	if claims.Expiry == 0 || now.After(time.Unix(claims.Expiry, 0)) {
		return GoogleIdentity{}, errors.New("auth: id_token has expired")
	}
	if claims.Subject == "" || claims.Email == "" {
		return GoogleIdentity{}, errors.New("auth: id_token is missing sub or email")
	}

	// email_verified has been seen as both a bool and the string "true".
	verified := false
	switch v := claims.Verified.(type) {
	case bool:
		verified = v
	case string:
		verified = v == "true"
	}

	email, err := NormaliseEmail(claims.Email)
	if err != nil {
		return GoogleIdentity{}, errors.New("auth: id_token email is not a valid address")
	}
	return GoogleIdentity{Subject: claims.Subject, Email: email, Verified: verified}, nil
}

// Upsert links the identity to a user, creating one if needed.
//
// Keyed on (provider, subject), never on email. An email can be reassigned
// inside a Google Workspace, and keying on it would hand the new holder of an
// address the previous holder's account here.
func (g *Google) Upsert(ctx context.Context, id GoogleIdentity, ip net.IP) (uuid.UUID, error) {
	if !id.Verified {
		return uuid.Nil, errs.Chassis().New(errs.CodeForbidden).
			WithMessage("That Google account's email address is not verified.").
			WithFix("Verify it with Google, or sign in with an email link instead.")
	}
	now := g.now().UTC()

	tx, err := g.pool.Unscoped().Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("auth: google upsert: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var userID uuid.UUID
	err = tx.QueryRow(ctx,
		`select user_id from oauth_identities where provider = 'google' and subject = $1`,
		id.Subject).Scan(&userID)
	switch {
	case err == nil:
		// Known identity. Refresh the asserted email, because Google is the
		// authority on it and support needs the current one.
		if _, err := tx.Exec(ctx, `
			update oauth_identities set email = $2, last_login_at = $3
			 where provider = 'google' and subject = $1`, id.Subject, id.Email, now); err != nil {
			return uuid.Nil, fmt.Errorf("auth: refresh identity: %w", err)
		}
	case errors.Is(err, db.ErrNoRows):
		// First Google sign-in. Link to an existing user with this address if
		// there is one — the address is verified, so this is the same person
		// arriving by a second door rather than a new account.
		err = tx.QueryRow(ctx, `
			insert into users (email) values ($1)
			on conflict (email) do update set email = excluded.email
			returning id`, id.Email).Scan(&userID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("auth: upsert user: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			insert into oauth_identities (provider, subject, user_id, email, last_login_at)
			values ('google', $1, $2, $3, $4)`, id.Subject, userID, id.Email, now); err != nil {
			return uuid.Nil, fmt.Errorf("auth: link identity: %w", err)
		}
	default:
		return uuid.Nil, fmt.Errorf("auth: look up identity: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("auth: google commit: %w", err)
	}
	_ = ip
	return userID, nil
}
