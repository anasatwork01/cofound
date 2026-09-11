// Package auth implements console sign-in: magic links, Google OAuth, and the
// rotating session cookie SPEC §8 requires.
//
// # Where this runs, and why
//
// §7.1 says auth is "via session cookie (console) or bearer PAT (future CLI)"
// and lists no /v1/auth endpoints, so `api` clearly VERIFIES sessions but
// nothing says who ISSUES them. §22 item 14 points at Auth.js / Better Auth,
// which are JavaScript and so imply the console. Sessions are issued here
// instead, and docs/open-questions.md Q4 records the reasoning: `api` has to
// verify them anyway, and the alternative is a Go service validating a session
// format a JavaScript library owns, sharing a table and a hashing scheme by
// convention rather than by contract.
//
// # What is stored
//
// Never a token. Only sha256(token), so a database dump yields nothing usable.
// The token itself exists in exactly two places: the cookie, and the one email
// containing a magic link.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
)

// tokenBytes is the entropy in a session token or a magic link.
//
// 32 bytes. Not a considered trade-off so much as the point past which
// guessing stops being the attack: at 256 bits, an attacker with the entire
// output of the sun for the lifetime of the sun does not enumerate the space.
// Anything smaller invites a conversation nobody should have to have.
const tokenBytes = 32

// Token is a bearer secret in its transportable form.
//
// A distinct type from string so that a token cannot be passed where a hash is
// wanted, and so that every place one is created or compared is greppable. Its
// String method is deliberately NOT the value; see below.
type Token struct{ raw string }

// Hash is sha256 of a token. This is what is stored and what is looked up by.
type Hash []byte

// NewToken returns a fresh token and its hash.
//
// Returning both is what stops a caller hashing it themselves with the wrong
// function, and what makes it obvious at the call site that the raw token has
// exactly one destination.
func NewToken() (Token, Hash, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing is not a recoverable condition: every token this
		// process would go on to mint is suspect. The caller turns this into a
		// 500 and the request is not served.
		return Token{}, nil, fmt.Errorf("auth: could not read random bytes: %w", err)
	}
	// base64url without padding: the token travels in a cookie and in a URL
	// query parameter, and '+', '/' and '=' each need escaping in one of those.
	raw := base64.RawURLEncoding.EncodeToString(buf)
	return Token{raw: raw}, HashToken(Token{raw: raw}), nil
}

// ParseToken wraps a token received from a request.
//
// Length is checked here rather than at every use. A token of the wrong length
// cannot be one we issued, and rejecting it before the database means a flood
// of junk cookies costs no queries.
func ParseToken(raw string) (Token, bool) {
	if len(raw) != base64.RawURLEncoding.EncodedLen(tokenBytes) {
		return Token{}, false
	}
	if _, err := base64.RawURLEncoding.DecodeString(raw); err != nil {
		return Token{}, false
	}
	return Token{raw: raw}, true
}

// HashToken returns the value stored in the database.
func HashToken(t Token) Hash {
	sum := sha256.Sum256([]byte(t.raw))
	return sum[:]
}

// Secret returns the raw token, for the cookie or the email and nowhere else.
//
// Named Secret rather than String so that every place the real value is needed
// says so, and is greppable.
func (t Token) Secret() string { return t.raw }

// String redacts.
//
// The first version of this type deliberately did NOT implement fmt.Stringer,
// on the reasoning that a Stringer would put the token into any log line that
// formatted a surrounding struct with %v. That reasoning was backwards, and
// TestTokenDoesNotStringify caught it: fmt reflects into UNEXPORTED fields, so
// `%v` on a struct holding a Token printed the raw token regardless. Not
// implementing Stringer left the hole open; implementing it closes it.
//
// GoString is here for the same reason, because %#v does not use String.
func (t Token) String() string {
	if t.raw == "" {
		return "Token()"
	}
	return "Token([redacted])"
}

// GoString redacts under %#v.
func (t Token) GoString() string { return t.String() }

// IsZero reports whether the token is unset.
func (t Token) IsZero() bool { return t.raw == "" }

// Equal compares two hashes in constant time.
//
// The database lookup is by hash and so does the real work; this exists for the
// comparisons that happen in memory, where a byte-by-byte compare would leak
// the position of the first difference through timing. Cheap enough that there
// is no reason to think about which comparisons those are.
func (h Hash) Equal(other Hash) bool {
	return subtle.ConstantTimeCompare(h, other) == 1
}

// sha256sum is the PKCE challenge's hash, kept beside the other hashing so
// there is one import of crypto/sha256 in this package.
func sha256sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}
