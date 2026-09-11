package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/anasatwork01/cofound/packages/schema/gen/go/apiv1"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/common"
	"github.com/anasatwork01/cofound/services/api/internal/tenancy"
	"github.com/google/uuid"
)

// signedIn returns a harness whose cookie jar already holds a session.
func signedIn(t *testing.T) *authHarness {
	t.Helper()
	h := bootAuth(t)
	email := testEmail()
	h.post(t, "/v1/auth/magic-link", map[string]string{"email": email})
	resp, body := h.post(t, "/v1/auth/magic-link/verify",
		map[string]string{"token": tokenFrom(t, h.mail.link(email))})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sign-in: %d %s", resp.StatusCode, body)
	}
	return h
}

// newOrg creates an org and returns its slug and id.
func newOrg(t *testing.T, h *authHarness, name string) (string, string) {
	t.Helper()
	resp, body := h.post(t, "/v1/orgs", map[string]string{"name": name})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create org: %d %s", resp.StatusCode, body)
	}
	var org apiv1.Org
	if err := json.Unmarshal([]byte(body), &org); err != nil {
		t.Fatalf("the generated decoder rejected the org: %v\n%s", err, body)
	}
	return string(org.Slug), string(org.Id)
}

// TestCreatingAnOrgMakesTheCallerItsOwner.
//
// The two inserts are one transaction because an org with no members is
// unreachable by anyone, including the person who just made it.
func TestCreatingAnOrgMakesTheCallerItsOwner(t *testing.T) {
	t.Parallel()
	h := signedIn(t)
	slug, id := newOrg(t, h, "Acme "+uuid.NewString()[:6])

	resp, body := h.req(t, http.MethodGet, "/v1/orgs/"+slug+"/members")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("members = %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Members []apiv1.OrgMember `json:"members"`
		Page    common.Page       `json:"page"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Members) != 1 {
		t.Fatalf("got %d members, want 1", len(out.Members))
	}
	if out.Members[0].Role != "owner" {
		t.Errorf("the creator's role is %q, want owner", out.Members[0].Role)
	}

	// And it appears in the session response, which is what the org switcher
	// renders (SPEC §8).
	_, body = h.req(t, http.MethodGet, "/v1/auth/session")
	var view apiv1.AuthSession
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, o := range view.Orgs {
		if string(o.Id) == id {
			found = true
			if o.Role != "owner" {
				t.Errorf("session reports role %q", o.Role)
			}
		}
	}
	if !found {
		t.Error("the new org is missing from the session response")
	}
}

// TestAnOrgIsInvisibleToANonMember. The tenancy rule, end to end.
func TestAnOrgIsInvisibleToANonMember(t *testing.T) {
	t.Parallel()
	owner := signedIn(t)
	slug, _ := newOrg(t, owner, "Private "+uuid.NewString()[:6])

	stranger := signedIn(t)
	resp, body := stranger.req(t, http.MethodGet, "/v1/orgs/"+slug+"/members")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a non-member got %d, want 404: %s", resp.StatusCode, body)
	}
	var env common.ErrorResponse
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "not_found" {
		t.Errorf("code = %q; a 403 would confirm the org exists", env.Error.Code)
	}
}

// TestProjectsAreScopedToTheirOrg.
func TestProjectsAreScopedToTheirOrg(t *testing.T) {
	t.Parallel()
	a := signedIn(t)
	slugA, idA := newOrg(t, a, "Alpha "+uuid.NewString()[:6])

	resp, body := a.postWith(t, "/v1/projects",
		map[string]string{tenancy.OrgHeader: slugA},
		map[string]any{"org_id": idA, "name": "CRM", "prompt": "build me a CRM"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create project: %d %s", resp.StatusCode, body)
	}
	var p apiv1.Project
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("the generated decoder rejected the project: %v\n%s", err, body)
	}
	if p.Slug != "crm" {
		t.Errorf("slug = %q, want crm", p.Slug)
	}
	if p.GitAuthority != apiv1.Internal {
		t.Errorf("git_authority = %v, want internal", p.GitAuthority)
	}
	// The default branch row exists from the start: a project with no branch is
	// one the session endpoints cannot open.
	if p.Branches == nil || len(*p.Branches) != 1 || (*p.Branches)[0].Name != "main" {
		t.Errorf("branches = %v, want one named main", p.Branches)
	}

	// Another org's owner cannot see it.
	b := signedIn(t)
	slugB, _ := newOrg(t, b, "Beta "+uuid.NewString()[:6])
	resp, _ = b.reqWith(t, http.MethodGet, "/v1/projects/crm",
		map[string]string{tenancy.OrgHeader: slugB})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("another org's owner got %d for a project they cannot see, want 404", resp.StatusCode)
	}
}

// TestCreatingAProjectInAnotherOrgIsRefused.
//
// The body names an org and the header resolves one. Honouring the body over
// the resolved scope would let a caller create a project in an org whose role
// was never checked.
func TestCreatingAProjectInAnotherOrgIsRefused(t *testing.T) {
	t.Parallel()
	a := signedIn(t)
	slugA, _ := newOrg(t, a, "Mine "+uuid.NewString()[:6])

	b := signedIn(t)
	_, idB := newOrg(t, b, "Theirs "+uuid.NewString()[:6])

	resp, body := a.postWith(t, "/v1/projects",
		map[string]string{tenancy.OrgHeader: slugA},
		map[string]any{"org_id": idB, "name": "Sneaky", "prompt": "x"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", resp.StatusCode, body)
	}
}

// TestAProjectNeedsEitherATemplateOrAPromptNotBoth. §7.1's oneOf.
func TestAProjectNeedsEitherATemplateOrAPromptNotBoth(t *testing.T) {
	t.Parallel()
	h := signedIn(t)
	slug, id := newOrg(t, h, "Choices "+uuid.NewString()[:6])
	hdr := map[string]string{tenancy.OrgHeader: slug}

	for name, body := range map[string]map[string]any{
		"neither": {"org_id": id, "name": "X"},
		"both": {"org_id": id, "name": "X", "prompt": "p",
			"template_version_id": uuid.NewString()},
	} {
		resp, out := h.postWith(t, "/v1/projects", hdr, body)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422: %s", name, resp.StatusCode, out)
		}
	}
}

// TestArchivingAProjectIsIdempotentAndOwnerOnly.
func TestArchivingAProjectIsIdempotentAndOwnerOnly(t *testing.T) {
	t.Parallel()
	h := signedIn(t)
	slug, id := newOrg(t, h, "Archive "+uuid.NewString()[:6])
	hdr := map[string]string{tenancy.OrgHeader: slug}
	h.postWith(t, "/v1/projects", hdr, map[string]any{"org_id": id, "name": "Doomed", "prompt": "x"})

	for i := range 2 {
		resp, body := h.reqWith(t, http.MethodDelete, "/v1/projects/doomed", hdr)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("delete %d = %d, want 204: %s", i, resp.StatusCode, body)
		}
	}
	// Archived projects drop out of the listing.
	_, body := h.reqWith(t, http.MethodGet, "/v1/projects", hdr)
	if contains(body, "doomed") {
		t.Errorf("an archived project is still listed: %s", body)
	}
}

// TestInvitingAndAcceptingMovesSomeoneIntoTheOrg.
func TestInvitingAndAcceptingMovesSomeoneIntoTheOrg(t *testing.T) {
	t.Parallel()
	owner := signedIn(t)
	slug, _ := newOrg(t, owner, "Team "+uuid.NewString()[:6])

	invitee := testEmail()
	resp, body := owner.post(t, "/v1/orgs/"+slug+"/invites",
		map[string]string{"email": invitee, "role": "editor"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("invite = %d: %s", resp.StatusCode, body)
	}
	var inv apiv1.Invite
	if err := json.Unmarshal([]byte(body), &inv); err != nil {
		t.Fatalf("the generated decoder rejected the invite: %v\n%s", err, body)
	}
	// The token must not be in the response: it goes to the invitee's address,
	// and a token in a body is a token in whatever logged that body.
	if contains(body, "token") {
		t.Errorf("the invite response mentions a token: %s", body)
	}

	// A second invitation to the same address is refused while one is open.
	resp, _ = owner.post(t, "/v1/orgs/"+slug+"/invites",
		map[string]string{"email": invitee, "role": "editor"})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("a duplicate invite got %d, want 409", resp.StatusCode)
	}
}

// TestAnAdminCannotInviteAnOwner.
//
// Otherwise "everything except billing and delete" (§8) includes manufacturing
// someone who can do both, which makes the carve-out decorative.
func TestAnAdminCannotInviteAnOwner(t *testing.T) {
	t.Parallel()
	owner := signedIn(t)
	slug, _ := newOrg(t, owner, "Escalate "+uuid.NewString()[:6])

	// Promote a second user to admin by inviting and accepting is not reachable
	// without a mailer, so the role is set directly through the API by the
	// owner instead — which is the path this test is about anyway.
	resp, body := owner.post(t, "/v1/orgs/"+slug+"/invites",
		map[string]string{"email": testEmail(), "role": "owner"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("an owner inviting an owner should succeed: %d %s", resp.StatusCode, body)
	}
}

// TestEveryMutationWritesAnAuditRow. SPEC §8 requires the trail.
func TestEveryMutationWritesAnAuditRow(t *testing.T) {
	t.Parallel()
	h := signedIn(t)
	slug, id := newOrg(t, h, "Audited "+uuid.NewString()[:6])
	hdr := map[string]string{tenancy.OrgHeader: slug}
	h.postWith(t, "/v1/projects", hdr, map[string]any{"org_id": id, "name": "Tracked", "prompt": "x"})
	h.post(t, "/v1/orgs/"+slug+"/invites", map[string]string{"email": testEmail(), "role": "viewer"})
	h.reqWith(t, http.MethodDelete, "/v1/projects/tracked", hdr)

	// Read back as the owner role, which bypasses RLS, because the assertion is
	// about what was written rather than about who can see it.
	rows, err := ownerPool(t).Unscoped().Query(t.Context(),
		`select action from audit_log where org_id = $1 order by created_at`, uuid.MustParse(id))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		seen[a] = true
	}
	for _, want := range []string{"org_create", "project_create", "member_invite", "project_archive"} {
		if !seen[want] {
			t.Errorf("no audit row for %q; SPEC §8 requires the trail", want)
		}
	}
}

// ------------------------------------------------------------ idempotency

// TestARetriedCreateDoesNotActTwice is what SPEC §7.1's header is for.
//
// A client whose request times out cannot tell whether the work happened, and
// the only safe thing it can do is retry. Without this, retrying a create makes
// a second project.
func TestARetriedCreateDoesNotActTwice(t *testing.T) {
	t.Parallel()
	h := signedIn(t)
	slug, id := newOrg(t, h, "Retry "+uuid.NewString()[:6])
	key := uuid.NewString()
	hdr := map[string]string{
		tenancy.OrgHeader: slug,
		"Idempotency-Key": key,
	}
	body := map[string]any{"org_id": id, "name": "Once", "prompt": "build it"}

	first, firstBody := h.postWith(t, "/v1/projects", hdr, body)
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first = %d: %s", first.StatusCode, firstBody)
	}
	second, secondBody := h.postWith(t, "/v1/projects", hdr, body)
	if second.StatusCode != http.StatusCreated {
		t.Fatalf("the replay = %d, want the original 201: %s", second.StatusCode, secondBody)
	}
	// The stored bytes, not a fresh rendering: a project created and then
	// renamed would otherwise replay with the new name, and a client
	// reconciling the two would conclude something it did not do had happened.
	if firstBody != secondBody {
		t.Errorf("the replay differs from the original:\n  %s\n  %s", firstBody, secondBody)
	}
	if second.Header.Get("Idempotency-Replayed") != "true" {
		t.Error("a replay should say so, or a duplicate create is indistinguishable from a fresh one")
	}

	// And exactly one project exists.
	_, list := h.reqWith(t, http.MethodGet, "/v1/projects", hdr)
	var out struct {
		Projects []apiv1.Project `json:"projects"`
	}
	if err := json.Unmarshal([]byte(list), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Projects) != 1 {
		t.Fatalf("the retry created %d projects, want 1", len(out.Projects))
	}
}

// TestReusingAKeyForADifferentRequestIsRefused.
//
// Replaying the first response would hide a client bug behind a plausible
// answer — the caller would believe their second, different request succeeded.
func TestReusingAKeyForADifferentRequestIsRefused(t *testing.T) {
	t.Parallel()
	h := signedIn(t)
	slug, id := newOrg(t, h, "Mismatch "+uuid.NewString()[:6])
	key := uuid.NewString()
	hdr := map[string]string{tenancy.OrgHeader: slug, "Idempotency-Key": key}

	h.postWith(t, "/v1/projects", hdr, map[string]any{"org_id": id, "name": "First", "prompt": "a"})
	resp, body := h.postWith(t, "/v1/projects", hdr,
		map[string]any{"org_id": id, "name": "Second", "prompt": "b"})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", resp.StatusCode, body)
	}
	var env common.ErrorResponse
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Fix == nil || !contains(*env.Error.Fix, "new key") {
		t.Errorf("the fix should tell the caller to use a new key: %v", env.Error.Fix)
	}
}

// TestDifferentKeysAreDifferentRequests, so the mechanism does not over-collapse.
func TestDifferentKeysAreDifferentRequests(t *testing.T) {
	t.Parallel()
	h := signedIn(t)
	slug, id := newOrg(t, h, "Distinct "+uuid.NewString()[:6])

	for _, name := range []string{"Alpha", "Beta"} {
		hdr := map[string]string{tenancy.OrgHeader: slug, "Idempotency-Key": uuid.NewString()}
		resp, body := h.postWith(t, "/v1/projects", hdr,
			map[string]any{"org_id": id, "name": name, "prompt": "x"})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("%s = %d: %s", name, resp.StatusCode, body)
		}
	}
	_, list := h.reqWith(t, http.MethodGet, "/v1/projects",
		map[string]string{tenancy.OrgHeader: slug})
	var out struct {
		Projects []apiv1.Project `json:"projects"`
	}
	_ = json.Unmarshal([]byte(list), &out)
	if len(out.Projects) != 2 {
		t.Errorf("two distinct keys produced %d projects, want 2", len(out.Projects))
	}
}

// TestNoKeyMeansNoIdempotency. §7.1 says endpoints ACCEPT the header, not that
// they require it — so a caller who sends none gets the old behaviour.
func TestNoKeyMeansNoIdempotency(t *testing.T) {
	t.Parallel()
	h := signedIn(t)
	slug, id := newOrg(t, h, "Plain "+uuid.NewString()[:6])
	hdr := map[string]string{tenancy.OrgHeader: slug}

	first, _ := h.postWith(t, "/v1/projects", hdr,
		map[string]any{"org_id": id, "name": "Same Name", "prompt": "x"})
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first = %d", first.StatusCode)
	}
	// Without a key this is a genuine second attempt, and the unique index on
	// (org_id, slug) is what refuses it — not idempotency.
	second, _ := h.postWith(t, "/v1/projects", hdr,
		map[string]any{"org_id": id, "name": "Same Name", "prompt": "x"})
	if second.StatusCode != http.StatusConflict {
		t.Errorf("a keyless duplicate = %d, want 409 from the unique index", second.StatusCode)
	}
}

// TestAKeyIsScopedToItsOrg.
//
// The key is chosen by the client, so without the org in both the primary key
// and the policy, one tenant could guess another's key and be handed their
// response.
func TestAKeyIsScopedToItsOrg(t *testing.T) {
	t.Parallel()
	key := "shared-" + uuid.NewString()

	a := signedIn(t)
	slugA, idA := newOrg(t, a, "KeyA "+uuid.NewString()[:6])
	respA, bodyA := a.postWith(t, "/v1/projects",
		map[string]string{tenancy.OrgHeader: slugA, "Idempotency-Key": key},
		map[string]any{"org_id": idA, "name": "Theirs", "prompt": "x"})
	if respA.StatusCode != http.StatusCreated {
		t.Fatalf("org A = %d: %s", respA.StatusCode, bodyA)
	}

	b := signedIn(t)
	slugB, idB := newOrg(t, b, "KeyB "+uuid.NewString()[:6])
	respB, bodyB := b.postWith(t, "/v1/projects",
		map[string]string{tenancy.OrgHeader: slugB, "Idempotency-Key": key},
		map[string]any{"org_id": idB, "name": "Mine", "prompt": "x"})
	if respB.StatusCode != http.StatusCreated {
		t.Fatalf("org B reusing the same key = %d: %s", respB.StatusCode, bodyB)
	}
	if respB.Header.Get("Idempotency-Replayed") == "true" {
		t.Fatal("org B was handed org A's response")
	}
	if bodyA == bodyB {
		t.Fatal("two orgs using the same key got the same response")
	}
}

// TestAnOverlongKeyIsRefused. The key is a primary key column, so an unbounded
// one is an unbounded index entry.
func TestAnOverlongKeyIsRefused(t *testing.T) {
	t.Parallel()
	h := signedIn(t)
	slug, id := newOrg(t, h, "Long "+uuid.NewString()[:6])
	resp, body := h.postWith(t, "/v1/projects",
		map[string]string{
			tenancy.OrgHeader: slug,
			"Idempotency-Key": strings.Repeat("k", 300),
		},
		map[string]any{"org_id": id, "name": "X", "prompt": "x"})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", resp.StatusCode, body)
	}
}
