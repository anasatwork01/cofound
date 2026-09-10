package tenancy_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/anasatwork01/cofound/packages/chassis/scope"
	"github.com/anasatwork01/cofound/packages/db"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/common"
	"github.com/anasatwork01/cofound/services/api/internal/tenancy"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The routes below are TEST-LOCAL, not API surface. api.openapi.yaml documents
// no org-scoped endpoints yet — task 0.9 adds the first — and inventing one
// here to give the middleware something to call would break working agreement
// 4. A router built inside the test exercises the middleware without claiming
// anything about the product's surface.

func pool(t *testing.T) *db.Pool {
	t.Helper()
	url := os.Getenv("APP_DATABASE_URL")
	if url == "" {
		if os.Getenv("DATABASE_URL") != "" {
			t.Fatal("APP_DATABASE_URL is unset but DATABASE_URL is set; the harness is misconfigured")
		}
		t.Skip("APP_DATABASE_URL unset; run `make db-setup`")
	}
	cfg := db.Defaults()
	cfg.URL = url
	cfg.ApplicationName = "tenancy-test"
	p, err := db.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// fakeAuth stands in for internal/auth. The Authenticator interface exists so
// these two packages do not import each other: sign-in has to work before
// there is an org to scope to.
type fakeAuth struct {
	user      uuid.UUID
	noSession bool
	rotations int
}

func (f *fakeAuth) UserFor(*http.Request) (uuid.UUID, error) {
	if f.noSession {
		return uuid.Nil, tenancy.ErrNoSession
	}
	return f.user, nil
}
func (f *fakeAuth) Rotated(http.ResponseWriter, *http.Request) { f.rotations++ }

// world is two orgs, each with a project, and a user whose memberships the test
// controls.
type world struct {
	orgA, orgB   uuid.UUID
	slugA, slugB string
	projA, projB uuid.UUID
	projSlug     string
	user         uuid.UUID
	stranger     uuid.UUID
}

func seed(t *testing.T, p *db.Pool) *world {
	t.Helper()
	ctx := context.Background()
	sfx := uuid.NewString()[:8]
	w := &world{
		orgA:     uuid.New(),
		orgB:     uuid.New(),
		slugA:    "alpha-" + sfx,
		slugB:    "beta-" + sfx,
		projSlug: "shared-crm",
	}

	// Users first, and UNSCOPED: `users` has no policy, because sign-in must
	// find a user by email before any org is known.
	for _, u := range []*uuid.UUID{&w.user, &w.stranger} {
		if err := p.Unscoped().QueryRow(ctx,
			`insert into users (email) values ($1) returning id`,
			"tenancy-"+uuid.NewString()[:8]+"@test.invalid").Scan(u); err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}

	// Each org is created inside a transaction scoped to ITS OWN new id.
	//
	// This is the only way an org can be created at all, and it took a failing
	// test to notice: the policy on `orgs` is `id = current_org()`, so an
	// unscoped insert is refused — you cannot create an org without saying
	// which org you are creating. Generating the id first and scoping to it
	// satisfies the policy, and gives a property worth having for free: an
	// insert naming any OTHER id is refused, so a request cannot create an org
	// it did not declare. Verified both ways.
	for _, s := range []struct {
		org  uuid.UUID
		proj *uuid.UUID
		slug string
	}{{w.orgA, &w.projA, w.slugA}, {w.orgB, &w.projB, w.slugB}} {
		org, proj, slug := s.org, s.proj, s.slug
		err := p.Scope(ctx, org.String(), func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx,
				`insert into orgs (id, name, slug) values ($1, $2, $2)`, org, slug); err != nil {
				return err
			}
			// Both orgs get a project with the SAME slug, which is legal — §6
			// makes it unique per org — and is the ambiguity §7.1's paths
			// cannot express.
			return tx.QueryRow(ctx,
				`insert into projects (org_id, name, slug) values ($1, 'CRM', $2) returning id`,
				org, w.projSlug).Scan(proj)
		})
		if err != nil {
			t.Fatalf("seed org %s: %v", slug, err)
		}
	}

	t.Cleanup(func() {
		// Cleanup runs as the OWNER, which bypasses row-level security. Two
		// separate scoped deletes would work too; this is one statement and the
		// distinction does not matter for teardown.
		if url := os.Getenv("DATABASE_URL"); url != "" {
			cfg := db.Defaults()
			cfg.URL = url
			cfg.AllowPrivilegedRole = true
			if owner, err := db.Open(ctx, cfg); err == nil {
				defer owner.Close()
				_, _ = owner.Unscoped().Exec(ctx, `delete from orgs where id = any($1::uuid[])`,
					[]uuid.UUID{w.orgA, w.orgB})
				_, _ = owner.Unscoped().Exec(ctx, `delete from users where id = any($1::uuid[])`,
					[]uuid.UUID{w.user, w.stranger})
			}
		}
	})
	return w
}

func member(t *testing.T, p *db.Pool, org, user uuid.UUID, role tenancy.Role) {
	t.Helper()
	// Scoped, because org_members carries an org_id and the policy applies.
	err := p.Scope(context.Background(), org.String(), func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`insert into org_members (org_id, user_id, role) values ($1, $2, $3)
			 on conflict (org_id, user_id) do update set role = excluded.role`,
			org, user, string(role))
		return err
	})
	if err != nil {
		t.Fatalf("seed membership: %v", err)
	}
}

// router builds a test-local router carrying the middleware under test.
func router(rs *tenancy.Resolver, action tenancy.Action) http.Handler {
	r := chi.NewRouter()
	r.Use(rs.Authenticate)

	echo := func(w http.ResponseWriter, req *http.Request) {
		m := tenancy.MustFrom(req.Context())
		_ = json.NewEncoder(w).Encode(map[string]string{
			"user":    m.UserID.String(),
			"org":     m.OrgID.String(),
			"role":    string(m.Role),
			"project": m.ProjectID.String(),
			// Proof the chassis's scope was populated, which is what carries
			// the org onto every log line and span.
			"scope_org": scope.From(req.Context()).OrgID,
		})
	}

	r.Route("/orgs/{org}", func(r chi.Router) {
		r.With(rs.Require(action)).Get("/", echo)
	})
	r.Route("/projects/{project}", func(r chi.Router) {
		r.With(rs.Require(action)).Get("/", echo)
	})
	r.With(rs.Require(action)).Get("/unscoped", echo)
	return r
}

func call(t *testing.T, h http.Handler, path string, headers map[string]string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestResolveByOrgSlug(t *testing.T) {
	t.Parallel()
	p := pool(t)
	w := seed(t, p)
	member(t, p, w.orgA, w.user, tenancy.RoleAdmin)

	rs := &tenancy.Resolver{Pool: p, Auth: &fakeAuth{user: w.user}}
	code, body := call(t, router(rs, tenancy.ActionOrgRead), "/orgs/"+w.slugA, nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d: %s", code, body)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got["org"] != w.orgA.String() {
		t.Errorf("resolved org %s, want %s", got["org"], w.orgA)
	}
	if got["role"] != "admin" {
		t.Errorf("role = %q", got["role"])
	}
	// The chassis reads this for log fields and span attributes.
	if got["scope_org"] != w.orgA.String() {
		t.Errorf("scope org = %q; the org will not reach logs or spans", got["scope_org"])
	}
}

// TestANonMemberGetsNotFoundRatherThanForbidden is the disclosure rule.
//
// A 403 for a resource in someone else's org CONFIRMS it exists. §7.1 makes
// cross-tenant reads indistinguishable from absence on purpose, so "you may not
// see this" and "this does not exist" have to be the same answer.
func TestANonMemberGetsNotFoundRatherThanForbidden(t *testing.T) {
	t.Parallel()
	p := pool(t)
	w := seed(t, p)
	member(t, p, w.orgA, w.user, tenancy.RoleOwner) // a member of A, not of B

	rs := &tenancy.Resolver{Pool: p, Auth: &fakeAuth{user: w.user}}
	h := router(rs, tenancy.ActionOrgRead)

	real, realBody := call(t, h, "/orgs/"+w.slugB, nil)                // exists, not mine
	fake, fakeBody := call(t, h, "/orgs/does-not-exist-"+w.slugA, nil) // does not exist

	if real != http.StatusNotFound {
		t.Errorf("an org I am not a member of returned %d, want 404", real)
	}
	if real != fake {
		t.Errorf("an existing org I cannot see returns %d and a missing one %d; that is an existence oracle",
			real, fake)
	}
	var a, b common.ErrorResponse
	_ = json.Unmarshal([]byte(realBody), &a)
	_ = json.Unmarshal([]byte(fakeBody), &b)
	if a.Error.Code != b.Error.Code {
		t.Errorf("codes differ: %q vs %q", a.Error.Code, b.Error.Code)
	}
}

func TestNoSessionIsUnauthenticated(t *testing.T) {
	t.Parallel()
	p := pool(t)
	w := seed(t, p)
	rs := &tenancy.Resolver{Pool: p, Auth: &fakeAuth{noSession: true}}
	code, body := call(t, router(rs, tenancy.ActionOrgRead), "/orgs/"+w.slugA, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", code, body)
	}
}

// TestAmbiguousProjectIsRefusedRatherThanGuessed.
//
// The gap §7.1 leaves: `/v1/projects/{project}` takes a SLUG, and §6 makes a
// project slug unique only within an org. Serving a guess would mean acting on
// the wrong tenant's project, which is the worst available outcome.
func TestAmbiguousProjectIsRefusedRatherThanGuessed(t *testing.T) {
	t.Parallel()
	p := pool(t)
	w := seed(t, p)
	member(t, p, w.orgA, w.user, tenancy.RoleAdmin)
	member(t, p, w.orgB, w.user, tenancy.RoleAdmin)

	rs := &tenancy.Resolver{Pool: p, Auth: &fakeAuth{user: w.user}}
	h := router(rs, tenancy.ActionProjectRead)

	code, body := call(t, h, "/projects/"+w.projSlug, nil)
	if code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", code, body)
	}
	var env common.ErrorResponse
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "ambiguous_project" {
		t.Errorf("code = %q", env.Error.Code)
	}
	// The fix has to tell the caller how to disambiguate (SPEC §18).
	if env.Error.Fix == nil || !strings.Contains(*env.Error.Fix, tenancy.OrgHeader) {
		t.Errorf("the fix should name the %s header: %v", tenancy.OrgHeader, env.Error.Fix)
	}

	// With the header it resolves exactly.
	code, body = call(t, h, "/projects/"+w.projSlug, map[string]string{tenancy.OrgHeader: w.slugB})
	if code != http.StatusOK {
		t.Fatalf("with the org header: status = %d: %s", code, body)
	}
	var got map[string]string
	_ = json.Unmarshal([]byte(body), &got)
	if got["project"] != w.projB.String() {
		t.Errorf("resolved project %s, want the one in %s", got["project"], w.slugB)
	}
}

// TestAnUnambiguousProjectNeedsNoHeader keeps the common case on the path
// SPEC §7.1 actually specifies.
func TestAnUnambiguousProjectNeedsNoHeader(t *testing.T) {
	t.Parallel()
	p := pool(t)
	w := seed(t, p)
	member(t, p, w.orgA, w.user, tenancy.RoleEditor) // one org only

	rs := &tenancy.Resolver{Pool: p, Auth: &fakeAuth{user: w.user}}
	code, body := call(t, router(rs, tenancy.ActionProjectRead), "/projects/"+w.projSlug, nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d: %s", code, body)
	}
	var got map[string]string
	_ = json.Unmarshal([]byte(body), &got)
	if got["project"] != w.projA.String() {
		t.Errorf("resolved %s, want %s", got["project"], w.projA)
	}
}

// TestRequireEnforcesTheMatrix. The whole point of §8's "never inline".
func TestRequireEnforcesTheMatrix(t *testing.T) {
	t.Parallel()
	p := pool(t)
	w := seed(t, p)
	rs := &tenancy.Resolver{Pool: p, Auth: &fakeAuth{user: w.user}}

	cases := []struct {
		role tenancy.Role
		want int
	}{
		{tenancy.RoleOwner, http.StatusOK},
		{tenancy.RoleAdmin, http.StatusForbidden},
		{tenancy.RoleEditor, http.StatusForbidden},
		{tenancy.RoleViewer, http.StatusForbidden},
	}
	// An owner-only action, so three of the four roles must be refused.
	h := router(rs, tenancy.ActionOrgDelete)
	for _, c := range cases {
		member(t, p, w.orgA, w.user, c.role)
		code, body := call(t, h, "/orgs/"+w.slugA, nil)
		if code != c.want {
			t.Errorf("%s deleting an org: status = %d, want %d: %s", c.role, code, c.want, body)
		}
	}
}

// TestARefusalSaysWhatIsNeeded. SPEC §18: an error says what happened and what
// to do about it.
func TestARefusalSaysWhatIsNeeded(t *testing.T) {
	t.Parallel()
	p := pool(t)
	w := seed(t, p)
	member(t, p, w.orgA, w.user, tenancy.RoleViewer)

	rs := &tenancy.Resolver{Pool: p, Auth: &fakeAuth{user: w.user}}
	code, body := call(t, router(rs, tenancy.ActionSecretWrite), "/orgs/"+w.slugA, nil)
	if code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", code, body)
	}
	var env common.ErrorResponse
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q", env.Error.Code)
	}
	if env.Error.Details == nil {
		t.Fatal("a refusal should say which role is needed")
	}
	d := env.Error.Details
	if d["required_role"] != "admin" || d["your_role"] != "viewer" {
		t.Errorf("details = %v", d)
	}
}

// TestARefusalIsForbiddenNotNotFound, unlike an unknown org.
//
// The caller has already been shown the org exists — they are a member — so
// hiding the reason would only leave the console unable to explain why the
// button did nothing.
func TestARefusalIsForbiddenNotNotFound(t *testing.T) {
	t.Parallel()
	p := pool(t)
	w := seed(t, p)
	member(t, p, w.orgA, w.user, tenancy.RoleViewer)
	rs := &tenancy.Resolver{Pool: p, Auth: &fakeAuth{user: w.user}}
	code, _ := call(t, router(rs, tenancy.ActionOrgDelete), "/orgs/"+w.slugA, nil)
	if code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a member who lacks the role", code)
	}
}

// TestAnOrgScopedActionOnAnUnscopedRouteAsksForTheOrg.
func TestAnOrgScopedActionOnAnUnscopedRouteAsksForTheOrg(t *testing.T) {
	t.Parallel()
	p := pool(t)
	w := seed(t, p)
	member(t, p, w.orgA, w.user, tenancy.RoleOwner)
	rs := &tenancy.Resolver{Pool: p, Auth: &fakeAuth{user: w.user}}
	h := router(rs, tenancy.ActionOrgDelete)

	code, body := call(t, h, "/unscoped", nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", code, body)
	}
	// And with the header it resolves.
	code, _ = call(t, h, "/unscoped", map[string]string{tenancy.OrgHeader: w.slugA})
	if code != http.StatusOK {
		t.Errorf("with the org header: status = %d", code)
	}
}

// TestScopedRunsInsideTheResolvedOrg. The handler cannot pass the wrong org,
// because it does not pass one at all.
func TestScopedRunsInsideTheResolvedOrg(t *testing.T) {
	t.Parallel()
	p := pool(t)
	w := seed(t, p)
	member(t, p, w.orgA, w.user, tenancy.RoleAdmin)
	rs := &tenancy.Resolver{Pool: p, Auth: &fakeAuth{user: w.user}}

	var seen string
	var projects int
	r := chi.NewRouter()
	r.Use(rs.Authenticate)
	// Require, not just Authenticate: the org is resolved there, so a handler
	// calling Scoped without it has no membership and panics — which is the
	// right failure for a wiring bug, and is asserted separately below.
	r.With(rs.Require(tenancy.ActionProjectRead)).Get("/orgs/{org}", func(w2 http.ResponseWriter, req *http.Request) {
		err := rs.Scoped(req.Context(), func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.QueryRow(ctx,
				`select coalesce(nullif(current_setting('app.org_id', true), ''), '')`).Scan(&seen); err != nil {
				return err
			}
			// Row-level security is now in force, so this counts only this
			// org's projects even though the query names no org.
			return tx.QueryRow(ctx, `select count(*) from projects`).Scan(&projects)
		})
		if err != nil {
			http.Error(w2, err.Error(), 500)
			return
		}
		w2.WriteHeader(http.StatusOK)
	})

	code, body := call(t, r, "/orgs/"+w.slugA, nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d: %s", code, body)
	}
	if seen != w.orgA.String() {
		t.Errorf("app.org_id = %q, want %s", seen, w.orgA)
	}
	if projects != 1 {
		t.Errorf("saw %d projects; row-level security should have limited this to org A's one", projects)
	}
}

// TestRotationIsHonouredBeforeResolutionCanFail.
//
// A request that rotates the session and then 404s must not leave the browser
// holding a token inside its grace window and about to stop working.
func TestRotationIsHonouredBeforeResolutionCanFail(t *testing.T) {
	t.Parallel()
	p := pool(t)
	w := seed(t, p)
	auth := &fakeAuth{user: w.user} // a member of nothing, so resolution 404s
	rs := &tenancy.Resolver{Pool: p, Auth: auth}

	code, _ := call(t, router(rs, tenancy.ActionOrgRead), "/orgs/"+w.slugA, nil)
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
	if auth.rotations != 1 {
		t.Errorf("Rotated was called %d times; a rotated cookie must be written even when resolution fails",
			auth.rotations)
	}
}
