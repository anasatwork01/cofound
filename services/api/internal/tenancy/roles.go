// Package tenancy resolves (user_id, org_id, role) once per request and answers
// every permission question from one table.
//
// SPEC §8: "Every request resolves to (user_id, org_id, role). Role checks in a
// single middleware, never inline." The second half is the part that is easy to
// erode — one `if role != "owner"` in a handler, and the matrix below stops
// being the answer to what a role can do. So the matrix is the only exported
// way to ask, `Require` is the only way to enforce, and roles_test.go asserts
// §8's four sentences against the table rather than against prose.
package tenancy

import (
	"fmt"
	"slices"
)

// Role is a membership role. The vocabulary is SPEC §8's, repeated in
// packages/schema/common.schema.json and in org_members' check constraint;
// tests/integration asserts all three agree.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)

// Roles in descending authority. Order matters: it is what makes "admin can do
// everything except billing and delete" expressible as a rank rather than as a
// list repeated per action.
var ranked = []Role{RoleOwner, RoleAdmin, RoleEditor, RoleViewer}

// Valid reports whether r is a known role.
func (r Role) Valid() bool { return slices.Contains(ranked, r) }

// Action is something a request wants to do.
//
// Named for the DOMAIN act rather than for the endpoint, because §8 grants
// permissions in domain terms — "build", "deploy preview", "propose" — and an
// action per route would mean re-deciding the matrix every time a route is
// added. The audit log's action names (§8's nine) are deliberately a different
// vocabulary: one records what happened, this decides what may.
type Action string

const (
	// Reading. Every role can read; `viewer` exists to be able to do only this.
	ActionOrgRead      Action = "org.read"
	ActionProjectRead  Action = "project.read"
	ActionTimelineRead Action = "timeline.read"
	ActionSecretRead   Action = "secret.read"

	// Building. §8: editor can "build, deploy preview, propose".
	ActionProjectCreate  Action = "project.create"
	ActionProjectUpdate  Action = "project.update"
	ActionBuild          Action = "build"
	ActionDeployPreview  Action = "deploy.preview"
	ActionProposalCreate Action = "proposal.create"
	ActionFileWrite      Action = "file.write"
	ActionRestore        Action = "restore"

	// Administration. §8: admin does "everything except billing/delete".
	ActionMemberInvite      Action = "member.invite"
	ActionMemberRemove      Action = "member.remove"
	ActionMemberRoleChange  Action = "member.role_change"
	ActionSecretWrite       Action = "secret.write"
	ActionDomainChange      Action = "domain.change"
	ActionDeployProduction  Action = "deploy.production"
	ActionRollback          Action = "rollback"
	ActionCapabilityInstall Action = "capability.install"
	ActionRepoLink          Action = "repo.link"

	// Spending. §8: "Approving anything that spends money requires owner or
	// admin." A separate group from administration so the rule is checkable as
	// one statement rather than inferred from a scattering of entries.
	ActionProposalApprove Action = "proposal.approve"
	ActionBudgetChange    Action = "budget.change"
	ActionAdsSpend        Action = "ads.spend"
	ActionDomainPurchase  Action = "domain.purchase"

	// Owner only. §8: owner has "billing, delete, transfer".
	ActionBillingRead    Action = "billing.read"
	ActionBillingWrite   Action = "billing.write"
	ActionCreditPurchase Action = "credit.purchase"
	ActionOrgDelete      Action = "org.delete"
	ActionOrgTransfer    Action = "org.transfer"
	ActionProjectDelete  Action = "project.delete"
)

// minimum is the least-authoritative role that may perform each action.
//
// A minimum rank rather than a set of roles, because §8 describes authority as
// nested: owner ⊇ admin ⊇ editor ⊇ viewer, with two exceptions carved out for
// the owner. Encoding it as ranks means a new action is one line and cannot
// accidentally grant editor something admin lacks.
//
// ActionBillingRead is the one place the nesting is deliberately broken: §8
// gives billing to `owner`, and an admin who can change everything but must not
// see the invoice is a real arrangement in companies that separate the two.
var minimum = map[Action]Role{
	ActionOrgRead:      RoleViewer,
	ActionProjectRead:  RoleViewer,
	ActionTimelineRead: RoleViewer,
	// Reading a secret is NOT a viewer action even though it is a read. §17.2
	// treats secret access as privileged and §8 audits "secret read/write",
	// which only makes sense if reading is restricted.
	ActionSecretRead: RoleAdmin,

	ActionProjectCreate:  RoleEditor,
	ActionProjectUpdate:  RoleEditor,
	ActionBuild:          RoleEditor,
	ActionDeployPreview:  RoleEditor,
	ActionProposalCreate: RoleEditor,
	ActionFileWrite:      RoleEditor,
	ActionRestore:        RoleEditor,

	ActionMemberInvite:      RoleAdmin,
	ActionMemberRemove:      RoleAdmin,
	ActionMemberRoleChange:  RoleAdmin,
	ActionSecretWrite:       RoleAdmin,
	ActionDomainChange:      RoleAdmin,
	ActionDeployProduction:  RoleAdmin,
	ActionRollback:          RoleAdmin,
	ActionCapabilityInstall: RoleAdmin,
	ActionRepoLink:          RoleAdmin,

	ActionProposalApprove: RoleAdmin,
	ActionBudgetChange:    RoleAdmin,
	ActionAdsSpend:        RoleAdmin,
	ActionDomainPurchase:  RoleAdmin,

	ActionBillingRead:    RoleOwner,
	ActionBillingWrite:   RoleOwner,
	ActionCreditPurchase: RoleOwner,
	ActionOrgDelete:      RoleOwner,
	ActionOrgTransfer:    RoleOwner,
	ActionProjectDelete:  RoleOwner,
}

// rank is a role's index in `ranked`. Lower is more authoritative.
func rank(r Role) (int, bool) {
	i := slices.Index(ranked, r)
	return i, i >= 0
}

// Can reports whether role may perform action.
//
// An unknown action returns FALSE, not an error and not true. A permission
// question about something the matrix has never heard of is a bug, and denying
// is the only safe direction: the alternative is that a typo in an action
// constant silently grants everyone access.
func Can(role Role, action Action) bool {
	need, known := minimum[action]
	if !known {
		return false
	}
	have, ok := rank(role)
	if !ok {
		return false
	}
	needRank, _ := rank(need)
	return have <= needRank
}

// Actions returns every action the matrix knows, sorted. For the audit test and
// for a future "what can I do" endpoint the console could use to hide controls
// rather than let the user click into a 403.
func Actions() []Action {
	out := make([]Action, 0, len(minimum))
	for a := range minimum {
		out = append(out, a)
	}
	slices.Sort(out)
	return out
}

// Roles returns the vocabulary, most authoritative first.
func Roles() []Role { return slices.Clone(ranked) }

// Minimum returns the least role that may perform action.
func Minimum(action Action) (Role, error) {
	r, ok := minimum[action]
	if !ok {
		return "", fmt.Errorf("tenancy: %q is not a known action", action)
	}
	return r, nil
}
