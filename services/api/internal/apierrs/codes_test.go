package apierrs_test

import (
	"encoding/json"
	"testing"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/common"

	"github.com/anasatwork01/cofound/services/api/internal/apierrs"
)

// TestDomainCodesDecodeThroughTheGeneratedType. A domain code that fails the
// wire pattern would break every client decoder at the moment something is
// already wrong.
func TestDomainCodesDecodeThroughTheGeneratedType(t *testing.T) {
	t.Parallel()
	for _, code := range apierrs.Catalog.Codes() {
		e := apierrs.Catalog.New(code)
		body, err := json.Marshal(e.Response("REQ1"))
		if err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		var back common.ErrorResponse
		if err := json.Unmarshal(body, &back); err != nil {
			t.Errorf("%s: the generated decoder rejected it: %v", code, err)
		}
	}
}

// TestDomainCatalogExtendsRatherThanReplaces, so a handler can still return a
// transport error like not_found.
func TestDomainCatalogExtendsTheChassisVocabulary(t *testing.T) {
	t.Parallel()
	for _, want := range []errs.Code{errs.CodeNotFound, errs.CodeForbidden, errs.CodeInternal} {
		if _, ok := apierrs.Catalog.Lookup(want); !ok {
			t.Errorf("chassis code %s is missing from the api catalogue", want)
		}
	}
	for _, want := range []errs.Code{"turn_running", "no_turn_running", "lease_held", "budget_exceeded"} {
		if _, ok := apierrs.Catalog.Lookup(want); !ok {
			t.Errorf("domain code %s is missing", want)
		}
	}
}

// TestDomainConstructorsCarryFixText, because SPEC 18 requires an error to say
// how to fix it and these are the ones a user hits most.
func TestDomainConstructorsCarryFixText(t *testing.T) {
	t.Parallel()
	for name, e := range map[string]*errs.Error{
		"turn_running":    apierrs.TurnRunning(),
		"no_turn_running": apierrs.NoTurnRunning(),
		"lease_held":      apierrs.LeaseHeld(),
		"budget_exceeded": apierrs.BudgetExceeded(),
	} {
		if e.Fix == "" {
			t.Errorf("%s has no fix text", name)
		}
		if e.Message == "" {
			t.Errorf("%s has no message", name)
		}
	}
	if got := apierrs.BudgetExceeded().Status; got != 402 {
		t.Errorf("budget_exceeded status = %d, want 402", got)
	}
}
