package v1

import (
	"fmt"
	"strings"
	"testing"
)

// The seed query in insertGeneratedProject looks for `slug = 'untitled-project'
// or slug like 'untitled-project-%'`. That is only correct if every name this
// file invents actually slugifies into that shape — and slugify() is a
// different function in a different file, so nothing but this test holds the
// two together. A rename of the placeholder that missed generatedSlug would
// restart the numbering at 1 on every create and 409 the second project,
// which is the bug this whole path exists to fix.
func TestGeneratedNamesMatchTheSeedQuery(t *testing.T) {
	t.Parallel()
	if got := slugify(generatedName(1)); got != generatedSlug {
		t.Fatalf("slugify(generatedName(1)) = %q, want %q", got, generatedSlug)
	}
	for n := 2; n <= 25; n++ {
		got := slugify(generatedName(n))
		want := fmt.Sprintf("%s-%d", generatedSlug, n)
		if got != want {
			t.Errorf("slugify(generatedName(%d)) = %q, want %q", n, got, want)
		}
		if !strings.HasPrefix(got, generatedSlug+"-") {
			t.Errorf("generatedName(%d) slugifies to %q, which the seed query's LIKE would miss", n, got)
		}
	}
}

// A generated slug that fails validSlug would be rejected by the same check a
// user-supplied name goes through, and the insert would fail with a validation
// error nobody can act on — the user never typed a name.
func TestGeneratedNamesAreValidSlugs(t *testing.T) {
	t.Parallel()
	for _, n := range []int{0, 1, 2, 9, 10, 99, 100, 1000} {
		s := slugify(generatedName(n))
		if !validSlug(s) {
			t.Errorf("generatedName(%d) -> %q, which validSlug rejects", n, s)
		}
	}
}

// Distinct numbers must produce distinct slugs, or the retry loop spins through
// eight attempts at the same colliding name and reports a conflict.
func TestGeneratedNamesAreDistinct(t *testing.T) {
	t.Parallel()
	seen := map[string]int{}
	for n := 1; n <= 50; n++ {
		s := slugify(generatedName(n))
		if prev, ok := seen[s]; ok {
			t.Fatalf("generatedName(%d) and generatedName(%d) both slugify to %q", prev, n, s)
		}
		seen[s] = n
	}
}

// n <= 1 collapses to the unnumbered name. The seed is a count, and a count can
// be zero, so generatedName(1) is the first thing tried on an empty org.
func TestTheFirstGeneratedNameIsUnnumbered(t *testing.T) {
	t.Parallel()
	for _, n := range []int{0, 1} {
		if got := generatedName(n); got != "Untitled project" {
			t.Errorf("generatedName(%d) = %q, want %q", n, got, "Untitled project")
		}
	}
}
