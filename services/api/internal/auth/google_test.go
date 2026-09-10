package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// idToken builds an unsigned JWT with the given claims.
//
// Unsigned on purpose: parseIDToken deliberately does not verify the signature,
// because the token arrives over our own authenticated TLS connection to
// Google's token endpoint rather than through the browser (OpenID Connect Core
// §3.1.3.7 item 6). These tests therefore exercise exactly what runs in
// production. The claim checks it DOES make are what this file is about.
func idToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc(body) + ".signature-not-checked"
}

func TestParseIDTokenAcceptsAWellFormedToken(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0).UTC()
	raw := idToken(t, map[string]any{
		"iss": "https://accounts.google.com",
		"aud": "client-123",
		"sub": "google-subject-abc",
		// Mixed case, to prove normalisation happens.
		"email":          "Amy@Example.COM",
		"email_verified": true,
		"exp":            now.Add(time.Hour).Unix(),
	})

	got, err := parseIDToken(raw, "client-123", now)
	if err != nil {
		t.Fatalf("parseIDToken: %v", err)
	}
	if got.Subject != "google-subject-abc" {
		t.Errorf("subject = %q", got.Subject)
	}
	if got.Email != "amy@example.com" {
		t.Errorf("email = %q, want it lowercased", got.Email)
	}
	if !got.Verified {
		t.Error("verified should be true")
	}
}

// TestParseIDTokenRejectsAnotherClientsToken is the confused-deputy check.
//
// Without the audience check, an id_token minted for ANY other Google client —
// anyone's app, obtained by anyone — would authenticate a user here.
func TestParseIDTokenRejectsAnotherClientsToken(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0).UTC()
	raw := idToken(t, map[string]any{
		"iss": "https://accounts.google.com",
		"aud": "somebody-elses-client",
		"sub": "s", "email": "a@b.test", "email_verified": true,
		"exp": now.Add(time.Hour).Unix(),
	})
	_, err := parseIDToken(raw, "client-123", now)
	if err == nil {
		t.Fatal("a token for another client was accepted")
	}
	if !strings.Contains(err.Error(), "another client") {
		t.Errorf("error should name the cause: %v", err)
	}
}

func TestParseIDTokenRejectsTheWrongIssuer(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0).UTC()
	raw := idToken(t, map[string]any{
		"iss": "https://accounts.evil.test",
		"aud": "client-123",
		"sub": "s", "email": "a@b.test", "email_verified": true,
		"exp": now.Add(time.Hour).Unix(),
	})
	if _, err := parseIDToken(raw, "client-123", now); err == nil {
		t.Fatal("a token from another issuer was accepted")
	}
}

func TestParseIDTokenAcceptsBothGoogleIssuerSpellings(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0).UTC()
	for _, iss := range []string{"https://accounts.google.com", "accounts.google.com"} {
		raw := idToken(t, map[string]any{
			"iss": iss, "aud": "client-123", "sub": "s",
			"email": "a@b.test", "email_verified": true,
			"exp": now.Add(time.Hour).Unix(),
		})
		if _, err := parseIDToken(raw, "client-123", now); err != nil {
			t.Errorf("issuer %q rejected: %v", iss, err)
		}
	}
}

func TestParseIDTokenRejectsAnExpiredToken(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0).UTC()
	raw := idToken(t, map[string]any{
		"iss": "https://accounts.google.com", "aud": "client-123", "sub": "s",
		"email": "a@b.test", "email_verified": true,
		"exp": now.Add(-time.Second).Unix(),
	})
	if _, err := parseIDToken(raw, "client-123", now); err == nil {
		t.Fatal("an expired token was accepted")
	}
}

func TestParseIDTokenRejectsAMissingExpiry(t *testing.T) {
	t.Parallel()
	// A token with no exp is a permanent credential. Treating a missing claim
	// as "no deadline" rather than "invalid" is the mistake this covers.
	now := time.Unix(1_700_000_000, 0).UTC()
	raw := idToken(t, map[string]any{
		"iss": "https://accounts.google.com", "aud": "client-123", "sub": "s",
		"email": "a@b.test", "email_verified": true,
	})
	if _, err := parseIDToken(raw, "client-123", now); err == nil {
		t.Fatal("a token with no expiry was accepted")
	}
}

// TestParseIDTokenReadsEitherSpellingOfEmailVerified.
//
// Google has emitted this as both a JSON boolean and the string "true". A
// parser that handles only the boolean silently reads the string as false and
// refuses every sign-in, which looks like a Google outage rather than a bug.
func TestParseIDTokenReadsEitherSpellingOfEmailVerified(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0).UTC()
	for _, v := range []any{true, "true"} {
		raw := idToken(t, map[string]any{
			"iss": "https://accounts.google.com", "aud": "client-123", "sub": "s",
			"email": "a@b.test", "email_verified": v,
			"exp": now.Add(time.Hour).Unix(),
		})
		got, err := parseIDToken(raw, "client-123", now)
		if err != nil {
			t.Fatalf("email_verified=%v: %v", v, err)
		}
		if !got.Verified {
			t.Errorf("email_verified=%v read as unverified", v)
		}
	}
}

func TestParseIDTokenCarriesUnverifiedThrough(t *testing.T) {
	t.Parallel()
	// parseIDToken reports it; Upsert is what refuses it. Keeping the refusal
	// in one place means the reason reaching the user is written once.
	now := time.Unix(1_700_000_000, 0).UTC()
	raw := idToken(t, map[string]any{
		"iss": "https://accounts.google.com", "aud": "client-123", "sub": "s",
		"email": "a@b.test", "email_verified": false,
		"exp": now.Add(time.Hour).Unix(),
	})
	got, err := parseIDToken(raw, "client-123", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verified {
		t.Error("verified should be false")
	}
}

func TestAuthorizeURLRequestsTheNarrowestScope(t *testing.T) {
	t.Parallel()
	g := NewGoogle(nil, GoogleConfig{
		ClientID: "client-123", ClientSecret: "s", RedirectURL: "https://c.test/cb",
	}, nil, nil)
	c, err := NewChallenge()
	if err != nil {
		t.Fatal(err)
	}
	u, err := g.AuthorizeURL(c)
	if err != nil {
		t.Fatal(err)
	}
	for want, why := range map[string]string{
		"scope=openid+email":         "only openid and email; a broader scope asks the user for more than we use",
		"code_challenge_method=S256": "S256, never plain: plain sends the verifier itself and defeats PKCE",
		"response_type=code":         "the authorization-code flow",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("authorize URL is missing %q (%s):\n%s", want, why, u)
		}
	}
	if strings.Contains(u, "profile") {
		t.Errorf("authorize URL requests the profile scope, which nothing here reads:\n%s", u)
	}
	// The verifier must never travel in the redirect; only its hash.
	if strings.Contains(u, c.Verifier) {
		t.Error("the PKCE verifier leaked into the authorize URL")
	}
}

func TestUnconfiguredGoogleIsNotAvailable(t *testing.T) {
	t.Parallel()
	g := NewGoogle(nil, GoogleConfig{}, nil, nil)
	if g.Configured() {
		t.Fatal("an unconfigured client reported itself available")
	}
	if _, err := g.AuthorizeURL(Challenge{}); err == nil {
		t.Fatal("AuthorizeURL should refuse when unconfigured")
	}
}
