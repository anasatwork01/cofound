// Package audit writes the trail SPEC §8 requires.
//
// §8: "Full audit log for: sign-in, role change, secret read/write, proposal
// decision, publish, rollback, domain change, credit purchase, member removal."
//
// Nine actions, and the list is the contract. `Required()` returns it, and
// audit_test.go asserts that every entry in it has a constructor here — so
// adding a tenth is a deliberate edit and dropping one fails the build. A trail
// with a gap at the action someone is investigating is not a trail.
//
// # What is never written
//
// A secret value, a token, or a request body. `target` is a jsonb column and
// the temptation is to put the whole thing in it; §17.2 says otherwise, and
// "secret read/write" being an audited action means these rows are exactly
// where a leaked value would be most damaging. Constructors take identifiers
// and names, never contents, so there is no call site that could pass one.
package audit

import (
	"context"
	"fmt"
	"net"
	"slices"

	"github.com/anasatwork01/cofound/packages/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Action is an audited event's stable name.
//
// A different vocabulary from tenancy.Action, deliberately: that one decides
// what a role MAY do, this one records what HAPPENED. They overlap but are not
// the same set — a sign-in is audited and has no permission, and a project read
// has a permission and is not audited.
type Action string

const (
	ActionSignIn           Action = "sign_in"
	ActionRoleChange       Action = "role_change"
	ActionSecretRead       Action = "secret_read"
	ActionSecretWrite      Action = "secret_write"
	ActionProposalDecision Action = "proposal_decision"
	ActionPublish          Action = "publish"
	ActionRollback         Action = "rollback"
	ActionDomainChange     Action = "domain_change"
	ActionCreditPurchase   Action = "credit_purchase"
	ActionMemberRemove     Action = "member_remove"
)

// Actions beyond §8's nine. Recorded because they are cheap and because the
// first question after "who removed this member" is usually "who invited them".
const (
	ActionOrgCreate      Action = "org_create"
	ActionMemberInvite   Action = "member_invite"
	ActionInviteAccept   Action = "invite_accept"
	ActionProjectCreate  Action = "project_create"
	ActionProjectArchive Action = "project_archive"
)

// Required is SPEC §8's list, verbatim.
//
// audit_test.go asserts every one has a constructor, so this slice is the
// specification made executable rather than a comment that can drift.
func Required() []Action {
	return []Action{
		ActionSignIn,
		ActionRoleChange,
		ActionSecretRead,
		ActionSecretWrite,
		ActionProposalDecision,
		ActionPublish,
		ActionRollback,
		ActionDomainChange,
		ActionCreditPurchase,
		ActionMemberRemove,
	}
}

// ActorKind matches common.schema.json's ActorKind and audit_log's constraint.
type ActorKind string

const (
	ActorUser     ActorKind = "user"
	ActorAgent    ActorKind = "agent"
	ActorSystem   ActorKind = "system"
	ActorExternal ActorKind = "external"
)

// Entry is one audited event.
type Entry struct {
	// OrgID is nil for events that happen before an org is known — a sign-in,
	// most obviously. audit_log's columns are nullable for exactly that.
	OrgID *uuid.UUID
	// ActorUserID is nil when the actor is not a person: an expiry sweeper, a
	// webhook, the agent.
	ActorUserID *uuid.UUID
	ActorKind   ActorKind
	Action      Action
	// Target identifies WHAT was acted on. Identifiers and names only, never
	// contents — see the package comment.
	Target    map[string]any
	IP        net.IP
	UserAgent string
}

// Writer appends to audit_log.
type Writer struct{ Pool *db.Pool }

// New returns a writer.
func New(pool *db.Pool) *Writer { return &Writer{Pool: pool} }

// Record appends one entry.
//
// Written UNSCOPED even when the entry has an org. audit_log carries a policy
// like every other tenant table, and a scoped write would work — but an audit
// row must be written even when the surrounding request is failing, including
// when it is failing because the caller's scope is wrong. Reading is scoped;
// writing is not.
func (w *Writer) Record(ctx context.Context, e Entry) error {
	if w == nil || w.Pool == nil {
		return nil
	}
	if e.Action == "" {
		return fmt.Errorf("audit: an entry needs an action")
	}
	if e.ActorKind == "" {
		e.ActorKind = ActorSystem
	}
	if e.Target == nil {
		e.Target = map[string]any{}
	}
	_, err := w.Pool.Unscoped().Exec(ctx, `
		insert into audit_log (org_id, actor_user_id, actor_kind, action, target, ip, user_agent)
		values ($1, $2, $3, $4, $5, $6, $7)`,
		e.OrgID, e.ActorUserID, string(e.ActorKind), string(e.Action), e.Target,
		nullableIP(e.IP), truncate(e.UserAgent, 512))
	if err != nil {
		return fmt.Errorf("audit: record %s: %w", e.Action, err)
	}
	return nil
}

// RecordTx appends inside an existing transaction.
//
// For the events that must be atomic with the change they describe: a role
// change whose audit row is missing because the request failed after the commit
// is worse than no audit at all, because it looks like the change never
// happened.
func (w *Writer) RecordTx(ctx context.Context, tx pgx.Tx, e Entry) error {
	if e.Action == "" {
		return fmt.Errorf("audit: an entry needs an action")
	}
	if e.ActorKind == "" {
		e.ActorKind = ActorSystem
	}
	if e.Target == nil {
		e.Target = map[string]any{}
	}
	_, err := tx.Exec(ctx, `
		insert into audit_log (org_id, actor_user_id, actor_kind, action, target, ip, user_agent)
		values ($1, $2, $3, $4, $5, $6, $7)`,
		e.OrgID, e.ActorUserID, string(e.ActorKind), string(e.Action), e.Target,
		nullableIP(e.IP), truncate(e.UserAgent, 512))
	if err != nil {
		return fmt.Errorf("audit: record %s: %w", e.Action, err)
	}
	return nil
}

// Actor is the common half of an entry, so a handler builds one and reuses it.
type Actor struct {
	UserID    uuid.UUID
	OrgID     uuid.UUID
	IP        net.IP
	UserAgent string
}

func (a Actor) entry(action Action, target map[string]any) Entry {
	e := Entry{ActorKind: ActorUser, Action: action, Target: target, IP: a.IP, UserAgent: a.UserAgent}
	if a.UserID != uuid.Nil {
		id := a.UserID
		e.ActorUserID = &id
	}
	if a.OrgID != uuid.Nil {
		id := a.OrgID
		e.OrgID = &id
	}
	return e
}

// ---------------------------------------------------------- §8's nine

// SignIn records an authentication. The org is unknown at sign-in.
func SignIn(actor Actor, method string) Entry {
	e := actor.entry(ActionSignIn, map[string]any{"method": method})
	e.OrgID = nil
	return e
}

// RoleChange records a membership role being changed.
func RoleChange(actor Actor, subject uuid.UUID, from, to string) Entry {
	return actor.entry(ActionRoleChange, map[string]any{
		"user_id": subject.String(), "from": from, "to": to,
	})
}

// SecretRead records a secret being read. The KEY, never the value.
func SecretRead(actor Actor, projectID uuid.UUID, env, key string) Entry {
	return actor.entry(ActionSecretRead, map[string]any{
		"project_id": projectID.String(), "env": env, "key": key,
	})
}

// SecretWrite records a secret being written. The KEY, never the value.
func SecretWrite(actor Actor, projectID uuid.UUID, env, key string) Entry {
	return actor.entry(ActionSecretWrite, map[string]any{
		"project_id": projectID.String(), "env": env, "key": key,
	})
}

// ProposalDecision records an approval or a decline (SPEC §15).
func ProposalDecision(actor Actor, proposalID uuid.UUID, kind, decision string) Entry {
	return actor.entry(ActionProposalDecision, map[string]any{
		"proposal_id": proposalID.String(), "kind": kind, "decision": decision,
	})
}

// Publish records a production deployment.
func Publish(actor Actor, projectID, deploymentID uuid.UUID, version int, sha string) Entry {
	return actor.entry(ActionPublish, map[string]any{
		"project_id": projectID.String(), "deployment_id": deploymentID.String(),
		"version": version, "sha": sha,
	})
}

// Rollback records the edge being repointed at an earlier deployment.
func Rollback(actor Actor, projectID, toDeployment uuid.UUID, toVersion int) Entry {
	return actor.entry(ActionRollback, map[string]any{
		"project_id": projectID.String(), "deployment_id": toDeployment.String(),
		"version": toVersion,
	})
}

// DomainChange records a domain being added, verified or removed.
func DomainChange(actor Actor, projectID uuid.UUID, hostname, change string) Entry {
	return actor.entry(ActionDomainChange, map[string]any{
		"project_id": projectID.String(), "hostname": hostname, "change": change,
	})
}

// CreditPurchase records credits being bought.
//
// `amount` is a decimal STRING, matching common.schema.json's Credits and §6's
// numeric(14,4). Recording money as a float in the audit trail would make the
// trail disagree with the ledger in the fourth decimal place, which is exactly
// where §16.5's reconciliation looks.
func CreditPurchase(actor Actor, amount, currency string, ref map[string]any) Entry {
	target := map[string]any{"amount": amount, "currency": currency}
	for k, v := range ref {
		target[k] = v
	}
	return actor.entry(ActionCreditPurchase, target)
}

// MemberRemove records a member being removed from an org.
func MemberRemove(actor Actor, subject uuid.UUID, role string) Entry {
	return actor.entry(ActionMemberRemove, map[string]any{
		"user_id": subject.String(), "role": role,
	})
}

// ------------------------------------------------------------- the rest

// OrgCreate records an org being created.
func OrgCreate(actor Actor, slug, name string) Entry {
	return actor.entry(ActionOrgCreate, map[string]any{"slug": slug, "name": name})
}

// MemberInvite records an invitation being sent.
func MemberInvite(actor Actor, inviteID uuid.UUID, email, role string) Entry {
	return actor.entry(ActionMemberInvite, map[string]any{
		"invite_id": inviteID.String(), "email": email, "role": role,
	})
}

// InviteAccept records an invitation being redeemed.
func InviteAccept(actor Actor, inviteID uuid.UUID, role string) Entry {
	return actor.entry(ActionInviteAccept, map[string]any{
		"invite_id": inviteID.String(), "role": role,
	})
}

// ProjectCreate records a project being created.
func ProjectCreate(actor Actor, projectID uuid.UUID, slug, name string) Entry {
	return actor.entry(ActionProjectCreate, map[string]any{
		"project_id": projectID.String(), "slug": slug, "name": name,
	})
}

// ProjectArchive records a project being archived.
//
// Archived, not deleted: §19.3 asks how long a suspended project's data is kept
// and how the user is warned before erasure, so erasure is a separate retention
// job and this is what the console's "delete" does.
func ProjectArchive(actor Actor, projectID uuid.UUID, slug string) Entry {
	return actor.entry(ActionProjectArchive, map[string]any{
		"project_id": projectID.String(), "slug": slug,
	})
}

// All returns every action this package can write, sorted.
func All() []Action {
	out := []Action{
		ActionSignIn, ActionRoleChange, ActionSecretRead, ActionSecretWrite,
		ActionProposalDecision, ActionPublish, ActionRollback, ActionDomainChange,
		ActionCreditPurchase, ActionMemberRemove,
		ActionOrgCreate, ActionMemberInvite, ActionInviteAccept,
		ActionProjectCreate, ActionProjectArchive,
	}
	slices.Sort(out)
	return out
}

func nullableIP(ip net.IP) any {
	if ip == nil {
		return nil
	}
	return ip.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
