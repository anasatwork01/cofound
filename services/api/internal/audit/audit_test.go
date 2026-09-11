package audit_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/anasatwork01/cofound/services/api/internal/audit"
	"github.com/google/uuid"
)

// TestEverySpecEightActionHasAConstructor makes SPEC §8's list executable.
//
// §8: "Full audit log for: sign-in, role change, secret read/write, proposal
// decision, publish, rollback, domain change, credit purchase, member removal."
//
// Nine items — ten actions, because "secret read/write" is two. Dropping one
// fails here rather than being discovered by whoever is investigating an
// incident and finds the trail silent at the interesting moment.
func TestEverySpecEightActionHasAConstructor(t *testing.T) {
	t.Parallel()
	// Every exported function in the package that returns an Entry, by the
	// action it produces. Built by CALLING them, so a constructor that exists
	// but writes the wrong action does not satisfy its requirement.
	actor := audit.Actor{UserID: uuid.New(), OrgID: uuid.New(), IP: net.ParseIP("203.0.113.1")}
	id := uuid.New()
	produced := map[audit.Action]bool{}
	for _, e := range []audit.Entry{
		audit.SignIn(actor, "magic_link"),
		audit.RoleChange(actor, id, "viewer", "admin"),
		audit.SecretRead(actor, id, "production", "STRIPE_SECRET_KEY"),
		audit.SecretWrite(actor, id, "production", "STRIPE_SECRET_KEY"),
		audit.ProposalDecision(actor, id, "budget_change", "approved"),
		audit.Publish(actor, id, id, 7, "deadbeef"),
		audit.Rollback(actor, id, id, 6),
		audit.DomainChange(actor, id, "example.test", "added"),
		audit.CreditPurchase(actor, "100.0000", "usd", nil),
		audit.MemberRemove(actor, id, "editor"),
	} {
		produced[e.Action] = true
	}

	for _, want := range audit.Required() {
		if !produced[want] {
			t.Errorf("SPEC §8 requires an audit entry for %q and no constructor produces one", want)
		}
	}
}

// TestTheAdditionalActionsAlsoWork. Not required by §8, recorded because they
// are cheap and because the first question after "who removed this member" is
// usually "who invited them".
func TestTheAdditionalActionsAlsoWork(t *testing.T) {
	t.Parallel()
	actor := audit.Actor{UserID: uuid.New(), OrgID: uuid.New()}
	id := uuid.New()
	entries := []audit.Entry{
		audit.OrgCreate(actor, "acme", "Acme"),
		audit.MemberInvite(actor, id, "amy@example.test", "editor"),
		audit.InviteAccept(actor, id, "editor"),
		audit.ProjectCreate(actor, id, "crm", "CRM"),
		audit.ProjectArchive(actor, id, "crm"),
	}
	known := map[audit.Action]bool{}
	for _, a := range audit.All() {
		known[a] = true
	}
	for _, e := range entries {
		if e.Action == "" {
			t.Error("a constructor produced an entry with no action")
		}
		if !known[e.Action] {
			t.Errorf("%q is produced by a constructor but missing from All()", e.Action)
		}
		if e.ActorKind != audit.ActorUser {
			t.Errorf("%q has actor kind %q, want user", e.Action, e.ActorKind)
		}
	}
}

// TestAllIsComplete. Every action a constructor can produce must be in All(),
// or a consumer enumerating actions silently misses one.
func TestAllIsComplete(t *testing.T) {
	t.Parallel()
	all := map[audit.Action]bool{}
	for _, a := range audit.All() {
		all[a] = true
	}
	for _, a := range audit.Required() {
		if !all[a] {
			t.Errorf("%q is required by §8 but missing from All()", a)
		}
	}
}

// TestRequiredMatchesSpecEightsWording guards the list itself.
//
// Required() is prose turned into code, and prose turned into code drifts back
// into prose unless something checks. This asserts the nine §8 names are all
// present, spelled the way the rest of the system spells them.
func TestRequiredMatchesSpecEightsWording(t *testing.T) {
	t.Parallel()
	want := []string{
		"sign_in", "role_change", "secret_read", "secret_write",
		"proposal_decision", "publish", "rollback", "domain_change",
		"credit_purchase", "member_remove",
	}
	var got []string
	for _, a := range audit.Required() {
		got = append(got, string(a))
	}
	slices.Sort(got)
	slices.Sort(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Required() is %v, want %v", got, want)
	}
}

// TestAConstructorNeverCarriesASecretValue.
//
// "secret read/write" being an audited action means these rows are exactly
// where a leaked value would be most damaging (§17.2). The constructors take
// identifiers and names, never contents — this asserts the shape rather than
// trusting the call sites.
func TestAConstructorNeverCarriesASecretValue(t *testing.T) {
	t.Parallel()
	actor := audit.Actor{UserID: uuid.New(), OrgID: uuid.New()}
	for _, e := range []audit.Entry{
		audit.SecretRead(actor, uuid.New(), "production", "STRIPE_SECRET_KEY"),
		audit.SecretWrite(actor, uuid.New(), "production", "STRIPE_SECRET_KEY"),
	} {
		// The key is recorded; anything resembling a value is not, because the
		// constructor has no parameter that could carry one.
		if e.Target["key"] != "STRIPE_SECRET_KEY" {
			t.Errorf("the key should be recorded: %v", e.Target)
		}
		for k, v := range e.Target {
			if s, ok := v.(string); ok && (strings.Contains(k, "value") || strings.Contains(s, "sk_")) {
				t.Errorf("target carries something value-shaped: %s = %v", k, v)
			}
		}
	}
}

// TestSignInHasNoOrg. A sign-in happens before any org is chosen, and §8 audits
// it precisely because it is the event with no tenant.
func TestSignInHasNoOrg(t *testing.T) {
	t.Parallel()
	e := audit.SignIn(audit.Actor{UserID: uuid.New(), OrgID: uuid.New()}, "google")
	if e.OrgID != nil {
		t.Errorf("a sign-in carried an org: %v", *e.OrgID)
	}
	if e.ActorUserID == nil {
		t.Error("a sign-in must record who signed in")
	}
}

// TestCreditPurchaseRecordsADecimalString.
//
// Money as a float in the audit trail would disagree with the ledger in the
// fourth decimal place, which is exactly where §16.5's reconciliation looks.
func TestCreditPurchaseRecordsADecimalString(t *testing.T) {
	t.Parallel()
	e := audit.CreditPurchase(audit.Actor{}, "100.5000", "usd", nil)
	if _, ok := e.Target["amount"].(string); !ok {
		t.Errorf("amount is %T, want a decimal string", e.Target["amount"])
	}
}

// TestEveryConstructorIsExercisedByThisFile.
//
// Parses the package's own source and asserts every exported function returning
// an Entry is called above. Without this, adding a constructor and forgetting
// to cover it is silent — and the completeness test would keep passing while
// covering less.
func TestEveryConstructorIsExercisedByThisFile(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	var constructors []string
	for name, pkg := range pkgs {
		if strings.HasSuffix(name, "_test") {
			continue
		}
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || !fn.Name.IsExported() || fn.Type.Results == nil {
					continue
				}
				for _, res := range fn.Type.Results.List {
					if ident, ok := res.Type.(*ast.Ident); ok && ident.Name == "Entry" {
						constructors = append(constructors, fn.Name.Name)
					}
				}
			}
		}
	}
	if len(constructors) == 0 {
		t.Fatal("no Entry constructors found; has the package moved?")
	}

	source, err := parseSelf(fset)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range constructors {
		if !strings.Contains(source, "audit."+name+"(") {
			t.Errorf("audit.%s is never exercised by audit_test.go", name)
		}
	}
}

func parseSelf(*token.FileSet) (string, error) {
	b, err := os.ReadFile("audit_test.go")
	return string(b), err
}
