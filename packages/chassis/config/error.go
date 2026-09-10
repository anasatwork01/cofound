package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Problem is one thing wrong with the environment.
//
// Expect describes the shape wanted. It never contains the value received: a
// configuration error is logged, and a malformed DATABASE_URL echoed into that
// log is a credential in a log file (SPEC 17.2).
type Problem struct {
	Key    string // environment variable, or "" for a cross-field rule
	Expect string
}

func (p Problem) Error() string {
	if p.Key == "" {
		return p.Expect
	}
	return p.Key + ": " + p.Expect
}

// Error is every problem found in one pass.
//
// Reporting them together is the whole point of the accumulator: a
// misconfigured deploy costs one cycle to diagnose, not one per variable.
type Error struct{ Problems []Problem }

func (e *Error) Error() string {
	if len(e.Problems) == 1 {
		return "configuration is invalid: " + e.Problems[0].Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "configuration is invalid (%d problems):", len(e.Problems))
	for _, p := range e.Problems {
		b.WriteString("\n  ")
		b.WriteString(p.Error())
	}
	return b.String()
}

// Unwrap exposes the problems to errors.Is and errors.As.
func (e *Error) Unwrap() []error {
	out := make([]error, len(e.Problems))
	for i, p := range e.Problems {
		out[i] = p
	}
	return out
}

// Keys returns the distinct variables named, sorted. Cross-field rules
// contribute no key.
func (e *Error) Keys() []string {
	seen := map[string]struct{}{}
	for _, p := range e.Problems {
		if p.Key != "" {
			seen[p.Key] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var _ error = (*Error)(nil)

// AsError returns the *Error in err, if any.
func AsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
