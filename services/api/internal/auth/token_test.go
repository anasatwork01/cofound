package auth

import (
	"fmt"
	"strings"
	"testing"
)

func TestNewTokenIsUnpredictableAndHashConsistent(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for range 200 {
		tok, hash, err := NewToken()
		if err != nil {
			t.Fatal(err)
		}
		if seen[tok.Secret()] {
			t.Fatal("NewToken repeated itself")
		}
		seen[tok.Secret()] = true
		if !HashToken(tok).Equal(hash) {
			t.Fatal("the returned hash is not the hash of the returned token")
		}
	}
}

// TestTokenRedactsItselfInEveryFormatVerb is the property that keeps tokens
// out of logs.
//
// This test found a real hole. The first version of Token deliberately did not
// implement fmt.Stringer, reasoning that a Stringer would embed the token in
// any log line formatting a surrounding struct with %v. Exactly backwards: fmt
// reflects into unexported fields, so %v printed the raw token anyway.
// Redacting in String and GoString is what actually closes it.
func TestTokenRedactsItselfInEveryFormatVerb(t *testing.T) {
	t.Parallel()
	tok, _, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	holder := struct {
		Session Token
		Note    string
	}{Session: tok, Note: "n"}

	// Every verb a careless log line or error message might use.
	for _, verb := range []string{"%v", "%s", "%+v", "%#v", "%q"} {
		for _, subject := range []any{tok, holder, &holder} {
			out := fmt.Sprintf(verb, subject)
			if strings.Contains(out, tok.Secret()) {
				t.Errorf("%s on %T leaked the token: %s", verb, subject, out)
			}
		}
	}
	// And the real value is still reachable where it is meant to be.
	if tok.Secret() == "" {
		t.Fatal("Secret returned nothing")
	}
}

func TestParseTokenRejectsTheWrongShape(t *testing.T) {
	t.Parallel()
	tok, _, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ParseToken(tok.Secret()); !ok {
		t.Fatal("a token we minted did not parse")
	}
	for name, raw := range map[string]string{
		"empty":      "",
		"short":      tok.Secret()[:10],
		"long":       tok.Secret() + "AAAA",
		"not base64": strings.Repeat("!", len(tok.Secret())),
		"padded b64": strings.Repeat("A", len(tok.Secret())-1) + "=",
	} {
		if _, ok := ParseToken(raw); ok {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestHashEqualIsLengthSafe(t *testing.T) {
	t.Parallel()
	// subtle.ConstantTimeCompare returns 0 for unequal lengths rather than
	// panicking, which is what makes it safe to hand attacker-controlled input.
	if Hash("abc").Equal(Hash("abcd")) {
		t.Error("different lengths compared equal")
	}
	if !Hash("abc").Equal(Hash("abc")) {
		t.Error("identical values compared unequal")
	}
	if Hash(nil).Equal(Hash("a")) {
		t.Error("nil compared equal to a value")
	}
}

func TestNormaliseEmail(t *testing.T) {
	t.Parallel()
	ok := map[string]string{
		"  Amy@Example.COM  ":     "amy@example.com",
		"Amy Smith <amy@ex.test>": "amy@ex.test",
		"a+tag@ex.test":           "a+tag@ex.test",
		"a.b@ex.test":             "a.b@ex.test",
	}
	for in, want := range ok {
		got, err := NormaliseEmail(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"", "   ", "not-an-email", "@ex.test", "a@", "a@b@c"} {
		if _, err := NormaliseEmail(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// TestNormaliseEmailDoesNotApplyProviderRules is a deliberate non-feature.
//
// Stripping dots or +suffixes is popular and wrong: those rules belong to
// particular providers, change without notice, and applying them collapses two
// distinct addresses at any provider that does not share the assumption.
// Under-normalising creates a duplicate account, which is recoverable.
// Over-normalising hands one person's account to another, which is not.
func TestNormaliseEmailDoesNotApplyProviderRules(t *testing.T) {
	t.Parallel()
	a, _ := NormaliseEmail("first.last@gmail.com")
	b, _ := NormaliseEmail("firstlast@gmail.com")
	if a == b {
		t.Fatal("dots were stripped; two distinct addresses now collapse to one account")
	}
	c, _ := NormaliseEmail("amy+halyard@gmail.com")
	d, _ := NormaliseEmail("amy@gmail.com")
	if c == d {
		t.Fatal("a +suffix was stripped")
	}
}
