package tenancy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/chassis/httpx"
	"github.com/anasatwork01/cofound/packages/chassis/scope"
	"github.com/anasatwork01/cofound/packages/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// OrgHeader lets a client say which org a request is about.
//
// SPEC §7.1's project paths are `/v1/projects/:p` where `:p` is a SLUG, and §6
// makes a project slug unique only WITHIN an org (`unique (org_id, slug)`). So
// for a user who belongs to two orgs that each have a project called `crm`,
// that path is ambiguous — and §7.1 defines no mechanism for saying which,
// even though §8 requires "org switching in the project picker", which means
// the console has a current org to send.
//
// This header is that mechanism. It is optional: a request without it resolves
// across the user's memberships and is refused only if genuinely ambiguous,
// which is what keeps single-org users — the overwhelming majority — on the
// path §7.1 actually specifies. docs/open-questions.md Q6 records the gap.
const OrgHeader = "X-Halyard-Org"

// Membership is the resolved tuple SPEC §8 asks for.
type Membership struct {
	UserID uuid.UUID
	OrgID  uuid.UUID
	Role   Role

	// OrgSlug and ProjectID are what the request named, resolved. Carried so a
	// handler does not re-query for what the middleware already looked up.
	OrgSlug   string
	ProjectID uuid.UUID
}

type membershipKey struct{}

// From returns the membership resolved for this request.
//
// The second return is false before the middleware has run, which is the state
// of every public route. A handler on an authenticated route can treat false as
// a programming error rather than a request problem.
func From(ctx context.Context) (Membership, bool) {
	m, ok := ctx.Value(membershipKey{}).(Membership)
	return m, ok
}

// MustFrom is From for a handler mounted behind Resolve.
func MustFrom(ctx context.Context) Membership {
	m, ok := From(ctx)
	if !ok {
		// Not a request error. A handler reaching this was mounted outside the
		// middleware, which is a wiring bug, and returning a 401 would hide it
		// as an auth problem the operator then cannot reproduce.
		panic("tenancy: no membership in context; is this handler mounted behind Require?")
	}
	return m
}

// Authenticator resolves a request's user. Implemented by internal/auth.
//
// An interface so this package does not import the auth package and the auth
// package does not import this one: sign-in has to work before there is an org
// to scope to, and a cycle between them would be the shape of that mistake.
type Authenticator interface {
	// UserFor returns the signed-in user, or ErrNoSession.
	UserFor(*http.Request) (uuid.UUID, error)
	// Rotated writes a refreshed session cookie if one was issued.
	Rotated(http.ResponseWriter, *http.Request)
}

// ErrNoSession is what an Authenticator returns for an unauthenticated request.
var ErrNoSession = errors.New("tenancy: no session")

// Resolver resolves and enforces tenancy.
type Resolver struct {
	Pool *db.Pool
	Auth Authenticator
}

// Authenticate resolves the caller, once, at the root of the authed subtree.
//
// It deliberately does NOT resolve the org, and that split was forced by a
// failing test rather than chosen up front. Middleware registered at a
// subtree's root runs BEFORE chi has matched the route below it, so
// chi.URLParam(r, "org") is empty there — every request came back "that request
// needs an organisation". Authentication needs no route parameters; org
// resolution needs them. So authentication happens here and org resolution
// happens in Require, which is inline on a route and therefore post-routing by
// construction. The chassis's RouteTag middleware carries the same note for the
// same reason.
//
// SPEC §8's "role checks in a single middleware, never inline" is still
// satisfied: Require is that single middleware, and it is the only thing that
// consults the matrix.
func (rs *Resolver) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, err := rs.Auth.UserFor(r)
		if err != nil {
			if errors.Is(err, ErrNoSession) {
				httpx.NewErrorWriter(nil).Fail(w, r, errs.Unauthenticated())
				return
			}
			httpx.NewErrorWriter(nil).Fail(w, r, err)
			return
		}
		// Honour a rotated session cookie before anything can fail: a request
		// that rotates and then 404s must not leave the browser holding a
		// token inside its grace window and about to stop working.
		rs.Auth.Rotated(w, r)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, userID)))
	})
}

func projectIDString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

type callerKey struct{}

// Caller returns the authenticated user, before any org is resolved.
//
// For the handful of authenticated routes that are not org-scoped at all —
// `POST /v1/orgs` creates one, and `GET /v1/templates` reads a global
// catalogue.
func Caller(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(callerKey{}).(uuid.UUID)
	return id, ok
}

// resolve works out which org the request is about. Called from Require, where
// route parameters are populated.
func (rs *Resolver) resolve(r *http.Request, userID uuid.UUID) (Membership, error) {
	ctx := r.Context()
	orgSlug := chi.URLParam(r, "org")
	projectSlug := chi.URLParam(r, "project")

	switch {
	case orgSlug != "":
		return rs.byOrgSlug(ctx, userID, orgSlug)
	case projectSlug != "":
		return rs.byProjectSlug(ctx, userID, projectSlug, r.Header.Get(OrgHeader))
	default:
		// A route with neither. `POST /v1/orgs` and `GET /v1/templates` are
		// authenticated but not org-scoped, so the caller is resolved and the
		// org is not.
		return rs.userOnly(ctx, userID, r.Header.Get(OrgHeader))
	}
}

// byOrgSlug resolves `/v1/orgs/{org}/...`. Org slugs are globally unique
// (§6: `orgs.slug ... unique`), so there is nothing ambiguous here.
func (rs *Resolver) byOrgSlug(ctx context.Context, userID uuid.UUID, slug string) (Membership, error) {
	m := Membership{UserID: userID, OrgSlug: slug}
	var role string
	// Inside a USER scope, not unscoped. `orgs` and `org_members` carry
	// policies, and migration 00016 makes them readable by the user they belong
	// to precisely so this lookup is possible — resolving which org a slug
	// names has to happen before any org is current, which is circular for an
	// org-keyed policy. An unscoped read here returns zero rows and reports
	// "that org does not exist" for every org that does; that is how the
	// circularity was found.
	err := rs.Pool.ScopeUser(ctx, userID.String(), func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			select o.id, m.role
			  from orgs o
			  join org_members m on m.org_id = o.id and m.user_id = $2
			 where o.slug = $1`, slug, userID).Scan(&m.OrgID, &role)
	})
	if err != nil {
		if errors.Is(err, db.ErrNoRows) {
			// One answer for "no such org" and "not your org". See Resolve.
			return Membership{}, errs.NotFound("org", slug)
		}
		return Membership{}, fmt.Errorf("tenancy: resolve org: %w", err)
	}
	m.Role = Role(role)
	if !m.Role.Valid() {
		// A role the database allows and this code does not know. Failing
		// closed rather than defaulting to viewer: the mismatch means the two
		// vocabularies have drifted, and guessing would make that invisible.
		return Membership{}, errs.Internal(fmt.Errorf("tenancy: unknown role %q on org %s", role, m.OrgID))
	}
	return m, nil
}

// byProjectSlug resolves `/v1/projects/{project}/...`.
//
// Archived projects ARE resolved. Excluding them made an archived project
// entirely unreachable — you could not GET one to see that it was archived,
// even though the Project schema has an `archived_at` field — and it made a
// repeated DELETE 404 rather than being idempotent. Which projects are hidden
// is a decision for each handler: listProjects filters, getProject does not.
//
// The ambiguity §7.1 leaves open is handled here rather than assumed away. With
// the org header, the lookup is exact. Without it, the project is resolved
// across the caller's memberships — and if more than one matches, the request
// is REFUSED rather than served against a guess. Picking one would mean acting
// on the wrong tenant's project, which is the worst available outcome.
func (rs *Resolver) byProjectSlug(ctx context.Context, userID uuid.UUID, slug, orgHint string) (Membership, error) {
	if orgHint != "" {
		return rs.byProjectAndOrg(ctx, userID, slug, orgHint)
	}

	// Without a hint, this takes two steps rather than one join, and the reason
	// is the row-level security policy on `projects`.
	//
	// The obvious single query — join projects to org_members and filter by
	// user — returns nothing, because `projects`' policy compares
	// `org_id = app.org_id` and no org is current yet. Widening that policy to
	// be user-aware, as migration 00016 did for `orgs` and `org_members`, would
	// work; it would also put a membership subquery on every project read
	// forever, for the sake of one lookup that happens once per request. The
	// measured cost of the current policy is 0.36 ms against 2.71 ms for a
	// subquery form (docs/verified.md), so that is not a trade worth making.
	//
	// So: list the caller's orgs, which IS readable in a user scope, then look
	// the project up inside each. One extra round trip for a single-org user,
	// which is almost everyone, and none at all once the console sends the org
	// header.
	candidates, err := rs.orgsFor(ctx, userID)
	if err != nil {
		return Membership{}, err
	}

	var found []Membership
	for _, c := range candidates {
		m := Membership{UserID: userID, OrgID: c.id, OrgSlug: c.slug, Role: c.role}
		err := rs.Pool.ScopeOrgAndUser(ctx, c.id.String(), userID.String(),
			func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx,
					`select id from projects where slug = $1`,
					slug).Scan(&m.ProjectID)
			})
		switch {
		case err == nil:
			found = append(found, m)
		case errors.Is(err, db.ErrNoRows):
			// This org has no such project. Not an error.
		default:
			return Membership{}, fmt.Errorf("tenancy: resolve project in %s: %w", c.slug, err)
		}
	}

	switch len(found) {
	case 0:
		return Membership{}, errs.NotFound("project", slug)
	case 1:
		if !found[0].Role.Valid() {
			return Membership{}, errs.Internal(fmt.Errorf("tenancy: unknown role %q", found[0].Role))
		}
		return found[0], nil
	default:
		slugs := make([]string, 0, len(found))
		for _, m := range found {
			slugs = append(slugs, m.OrgSlug)
		}
		// The org slugs ARE safe to name: the caller is a member of every one
		// of them, so this reveals nothing they cannot already list.
		return Membership{}, errs.Conflict("ambiguous_project",
			fmt.Sprintf("You have a project called %q in more than one organisation.", slug)).
			WithFix(fmt.Sprintf("Send the %s header with one of: %s.",
				OrgHeader, strings.Join(slugs, ", "))).
			WithDetail("orgs", slugs)
	}
}

// candidate is one org the caller belongs to.
type candidate struct {
	id   uuid.UUID
	slug string
	role Role
}

// orgsFor lists the orgs a user is a member of.
//
// Readable in a user scope because migration 00016 made `orgs` and
// `org_members` user-aware. This is also what the org switcher and the sign-in
// response need (§8), so it is not overhead invented for project resolution.
func (rs *Resolver) orgsFor(ctx context.Context, userID uuid.UUID) ([]candidate, error) {
	var out []candidate
	err := rs.Pool.ScopeUser(ctx, userID.String(), func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select o.id, o.slug, m.role
			  from org_members m
			  join orgs o on o.id = m.org_id
			 where m.user_id = $1
			 order by o.slug`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c candidate
			var role string
			if err := rows.Scan(&c.id, &c.slug, &role); err != nil {
				return err
			}
			c.role = Role(role)
			out = append(out, c)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("tenancy: list orgs: %w", err)
	}
	return out, nil
}

func (rs *Resolver) byProjectAndOrg(ctx context.Context, userID uuid.UUID, slug, orgSlug string) (Membership, error) {
	// Resolve the org first — which also verifies membership and yields the
	// role — then look the project up inside it. `projects` has an org-keyed
	// policy, so the read has to happen in that org's scope; see byProjectSlug
	// for why the policy is left that way.
	m, err := rs.byOrgSlug(ctx, userID, orgSlug)
	if err != nil {
		// An org the caller cannot see is reported as the PROJECT being
		// absent, not the org. The caller asked for a project; telling them
		// the org does not exist would confirm which of the two they got wrong.
		return Membership{}, errs.NotFound("project", slug)
	}

	err = rs.Pool.ScopeOrgAndUser(ctx, m.OrgID.String(), userID.String(),
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`select id from projects where slug = $1`,
				slug).Scan(&m.ProjectID)
		})
	if err != nil {
		if errors.Is(err, db.ErrNoRows) {
			return Membership{}, errs.NotFound("project", slug)
		}
		return Membership{}, fmt.Errorf("tenancy: resolve project in org: %w", err)
	}
	return m, nil
}

// userOnly resolves a route that authenticates but names no org.
//
// The org header is honoured when present, so `POST /v1/projects` knows which
// org to create in. Without it the membership carries a user and no org, and a
// handler that needs one asks for it explicitly.
func (rs *Resolver) userOnly(ctx context.Context, userID uuid.UUID, orgHint string) (Membership, error) {
	if orgHint == "" {
		return Membership{UserID: userID}, nil
	}
	return rs.byOrgSlug(ctx, userID, orgHint)
}

// Require resolves the org and refuses a request whose role may not perform
// action.
//
// This is the only enforcement point, and the only place the matrix is
// consulted. A handler must not ask `Can` itself: §8 says role checks live in
// one middleware, and the moment one handler decides for itself the matrix
// stops being the answer to what a role can do.
//
// Failure modes, and the one that matters:
//
//   - no session          401 (from Authenticate, upstream)
//   - org/project unknown 404
//   - NOT a member        404, deliberately NOT 403
//   - a member who lacks the role  403
//
// The third is the disclosure rule. A 403 for a resource in someone else's org
// confirms it exists; §7.1 makes cross-tenant reads indistinguishable from
// absence on purpose. The fourth is 403 because the caller has already been
// shown the org exists — they are a member — so hiding the reason would only
// leave the console unable to explain why the button did nothing.
func (rs *Resolver) Require(action Action) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ew := httpx.NewErrorWriter(nil)

			userID, ok := Caller(r.Context())
			if !ok {
				// Not a request error: the route was mounted outside
				// Authenticate, which is a wiring bug. A 401 would disguise it
				// as an auth problem the operator cannot reproduce.
				panic("tenancy: Require used outside Authenticate")
			}

			m, err := rs.resolve(r, userID)
			if err != nil {
				ew.Fail(w, r, err)
				return
			}
			if m.OrgID == uuid.Nil {
				ew.Fail(w, r, errs.Invalid("That request needs an organisation.").
					WithFix(fmt.Sprintf("Send the %s header.", OrgHeader)))
				return
			}
			if !Can(m.Role, action) {
				need, _ := Minimum(action)
				ew.Fail(w, r, errs.Forbidden(verb(action)).
					WithFix(fmt.Sprintf("Ask an owner or admin; this needs the %s role or higher.", need)).
					WithDetail("required_role", string(need)).
					WithDetail("your_role", string(m.Role)))
				return
			}

			ctx := context.WithValue(r.Context(), membershipKey{}, m)
			// The chassis reads this for log fields and span attributes, so the
			// org reaches every line and every span from here on without a call
			// site remembering to add it.
			ctx = scope.With(ctx, scope.Scope{
				OrgID:     m.OrgID.String(),
				ProjectID: projectIDString(m.ProjectID),
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// verb turns an action into something that reads in "Your role does not allow
// you to ...". SPEC §18: an error says what happened, in the interface's voice.
func verb(a Action) string {
	return strings.ReplaceAll(strings.ReplaceAll(string(a), ".", " "), "_", " ")
}

// Scoped runs fn inside a transaction scoped to the request's org.
//
// The only way a handler should reach the database for tenant data. It takes
// the org from the resolved membership rather than from an argument, so a
// handler cannot pass the wrong one — and row-level security then applies on
// top, as defence in depth rather than as the primary control (§6).
func (rs *Resolver) Scoped(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	m, ok := From(ctx)
	if !ok {
		panic("tenancy: Scoped used outside Require")
	}
	if m.OrgID == uuid.Nil {
		return errs.Invalid("That request needs an organisation.").
			WithFix(fmt.Sprintf("Send the %s header.", OrgHeader))
	}
	// Both, so `orgs` and `org_members` admit the caller's own rows as well as
	// the current org's (migration 00016).
	return rs.Pool.ScopeOrgAndUser(ctx, m.OrgID.String(), m.UserID.String(), fn)
}
