package tenancy_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anasatwork01/cofound/services/api/internal/tenancy"
)

// SPEC §8 defines the roles in four sentences. Each is asserted here against
// the matrix rather than against prose, so a matrix edit that contradicts the
// specification fails the build instead of becoming the new specification.
//
//	"Roles: owner (billing, delete, transfer), admin (everything except
//	 billing/delete), editor (build, deploy preview, propose), viewer (read)."
//	"Approving anything that spends money requires owner or admin."

func TestOwnerCanDoEverything(t *testing.T) {
	t.Parallel()
	for _, a := range tenancy.Actions() {
		if !tenancy.Can(tenancy.RoleOwner, a) {
			t.Errorf("owner cannot %q, but owner is the top of the hierarchy", a)
		}
	}
}

// TestAdminCanDoEverythingExceptBillingAndDelete is §8's second clause.
func TestAdminCanDoEverythingExceptBillingAndDelete(t *testing.T) {
	t.Parallel()
	ownerOnly := map[tenancy.Action]bool{
		tenancy.ActionBillingRead:    true,
		tenancy.ActionBillingWrite:   true,
		tenancy.ActionCreditPurchase: true,
		tenancy.ActionOrgDelete:      true,
		tenancy.ActionOrgTransfer:    true,
		tenancy.ActionProjectDelete:  true,
	}
	for _, a := range tenancy.Actions() {
		can := tenancy.Can(tenancy.RoleAdmin, a)
		switch {
		case ownerOnly[a] && can:
			t.Errorf("admin can %q, but §8 reserves billing, delete and transfer to owner", a)
		case !ownerOnly[a] && !can:
			t.Errorf("admin cannot %q, but §8 gives admin everything except billing and delete", a)
		}
	}
}

// TestEditorCanBuildDeployPreviewAndPropose is §8's third clause, and its
// boundary: an editor must NOT be able to administer or spend.
func TestEditorCanBuildDeployPreviewAndPropose(t *testing.T) {
	t.Parallel()
	for _, a := range []tenancy.Action{
		tenancy.ActionBuild,
		tenancy.ActionDeployPreview,
		tenancy.ActionProposalCreate,
	} {
		if !tenancy.Can(tenancy.RoleEditor, a) {
			t.Errorf("editor cannot %q, which §8 grants explicitly", a)
		}
	}
	for _, a := range []tenancy.Action{
		tenancy.ActionMemberInvite,
		tenancy.ActionMemberRemove,
		tenancy.ActionSecretWrite,
		tenancy.ActionDeployProduction,
		tenancy.ActionDomainChange,
		tenancy.ActionProposalApprove,
		tenancy.ActionBillingRead,
		tenancy.ActionOrgDelete,
	} {
		if tenancy.Can(tenancy.RoleEditor, a) {
			t.Errorf("editor can %q; §8 grants editor only build, deploy preview and propose", a)
		}
	}
}

// TestViewerCanOnlyRead is §8's fourth clause.
func TestViewerCanOnlyRead(t *testing.T) {
	t.Parallel()
	allowed := map[tenancy.Action]bool{
		tenancy.ActionOrgRead:      true,
		tenancy.ActionProjectRead:  true,
		tenancy.ActionTimelineRead: true,
	}
	for _, a := range tenancy.Actions() {
		can := tenancy.Can(tenancy.RoleViewer, a)
		if can && !allowed[a] {
			t.Errorf("viewer can %q; §8 gives viewer read only", a)
		}
		if !can && allowed[a] {
			t.Errorf("viewer cannot %q, which is a read", a)
		}
	}
}

// TestSpendingRequiresOwnerOrAdmin is §8's separate sentence: "Approving
// anything that spends money requires owner or admin."
func TestSpendingRequiresOwnerOrAdmin(t *testing.T) {
	t.Parallel()
	spending := []tenancy.Action{
		tenancy.ActionProposalApprove,
		tenancy.ActionBudgetChange,
		tenancy.ActionAdsSpend,
		tenancy.ActionDomainPurchase,
		tenancy.ActionCreditPurchase,
	}
	for _, a := range spending {
		for _, r := range []tenancy.Role{tenancy.RoleEditor, tenancy.RoleViewer} {
			if tenancy.Can(r, a) {
				t.Errorf("%s can %q, which spends money", r, a)
			}
		}
		if !tenancy.Can(tenancy.RoleAdmin, a) && a != tenancy.ActionCreditPurchase {
			t.Errorf("admin cannot %q, but §8 allows owner OR admin to approve spending", a)
		}
	}
}

// TestReadingASecretIsNotAViewerAction records a deviation from the plain
// reading of §8, so it stays a decision rather than becoming a surprise.
//
// §8 says viewer can "read". Taken literally that includes reading a secret,
// which §17.2 treats as privileged and which §8 itself audits as
// "secret read/write" — an audit that only makes sense if reading is
// restricted. Secret reads therefore need admin.
func TestReadingASecretIsNotAViewerAction(t *testing.T) {
	t.Parallel()
	for _, r := range []tenancy.Role{tenancy.RoleViewer, tenancy.RoleEditor} {
		if tenancy.Can(r, tenancy.ActionSecretRead) {
			t.Errorf("%s can read secrets; §17.2 treats that as privileged", r)
		}
	}
	if !tenancy.Can(tenancy.RoleAdmin, tenancy.ActionSecretRead) {
		t.Error("admin cannot read secrets, which leaves nobody but owner able to")
	}
}

// TestAuthorityIsNested. Every action a lower role may perform, every higher
// role may too — with the owner-only carve-outs as the only exceptions.
func TestAuthorityIsNested(t *testing.T) {
	t.Parallel()
	roles := tenancy.Roles() // most authoritative first
	for _, a := range tenancy.Actions() {
		// Find the least authoritative role that can do it, then assert every
		// role above it can as well.
		least := -1
		for i := len(roles) - 1; i >= 0; i-- {
			if tenancy.Can(roles[i], a) {
				least = i
				break
			}
		}
		if least < 0 {
			t.Errorf("no role can %q; the action is unreachable", a)
			continue
		}
		for i := 0; i < least; i++ {
			if !tenancy.Can(roles[i], a) {
				t.Errorf("%s cannot %q but %s can; authority is not nested",
					roles[i], a, roles[least])
			}
		}
	}
}

// TestAnUnknownActionIsDenied. A typo in an action constant must not grant
// access; denying is the only safe direction.
func TestAnUnknownActionIsDenied(t *testing.T) {
	t.Parallel()
	for _, r := range tenancy.Roles() {
		if tenancy.Can(r, tenancy.Action("nonsense.invented")) {
			t.Errorf("%s was granted an unknown action", r)
		}
	}
	if _, err := tenancy.Minimum(tenancy.Action("nonsense.invented")); err == nil {
		t.Error("Minimum should report an unknown action rather than defaulting")
	}
}

func TestAnUnknownRoleIsDenied(t *testing.T) {
	t.Parallel()
	// A role the database allows and this code does not know must be refused,
	// not defaulted to viewer: defaulting would hide the vocabularies drifting.
	if tenancy.Can(tenancy.Role("superadmin"), tenancy.ActionOrgRead) {
		t.Error("an unknown role was granted a read")
	}
	if tenancy.Role("superadmin").Valid() {
		t.Error("an unknown role reported itself valid")
	}
}

// TestTheRoleVocabularyMatchesTheSharedContract.
//
// org_members' check constraint, common.schema.json's Role, and this package
// must agree. tests/integration asserts the first two against each other; this
// is the third corner.
func TestTheRoleVocabularyMatchesTheSharedContract(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "packages/schema/common.schema.json"))
	if err != nil {
		t.Fatalf("read the shared schema: %v", err)
	}
	var doc struct {
		Defs struct {
			Role struct {
				Enum []string `json:"enum"`
			} `json:"Role"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Defs.Role.Enum) == 0 {
		t.Fatal("no Role enum found in common.schema.json")
	}

	var mine []string
	for _, r := range tenancy.Roles() {
		mine = append(mine, string(r))
	}
	slices.Sort(mine)
	fromSchema := slices.Clone(doc.Defs.Role.Enum)
	slices.Sort(fromSchema)

	if strings.Join(mine, ",") != strings.Join(fromSchema, ",") {
		t.Errorf("this package knows %v but common.schema.json's Role is %v", mine, fromSchema)
	}
}
