// Package v1 implements the /v1 surface documented in
// packages/schema/api.openapi.yaml.
//
// Every handler returns an error rather than writing one, so the chassis's
// single ErrorWriter is the only thing that renders a body — a handler that let
// a driver error escape would produce a canned envelope rather than a
// connection string (packages/chassis/errs).
//
// Every tenant read and write goes through tenancy.Scoped, so row-level
// security applies as defence in depth on top of the explicit filtering §6
// requires. There is no exported way to reach the database otherwise.
package v1

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/chassis/httpx"
	"github.com/anasatwork01/cofound/packages/db"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/apiv1"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/common"
	"github.com/anasatwork01/cofound/services/api/internal/audit"
	"github.com/anasatwork01/cofound/services/api/internal/tenancy"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// maxBody bounds a /v1 request body. The chassis applies HTTP_MAX_BODY_BYTES
// as well; this is the per-endpoint bound for bodies that are a handful of
// short fields.
const maxBody = 64 << 10

// createOrg implements POST /v1/orgs.
//
// The caller becomes its owner, in the same transaction. An org with no members
// is unreachable by anyone — including the person who just made it — so the two
// inserts cannot be allowed to come apart.
func (h *Handlers) createOrg(w http.ResponseWriter, r *http.Request) error {
	userID, ok := tenancy.Caller(r.Context())
	if !ok {
		return errs.Unauthenticated()
	}
	body, err := httpx.Decode[apiv1.CreateOrgRequest](r, maxBody)
	if err != nil {
		return err
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		return errs.InvalidField("name", "is required")
	}
	slug := ""
	if body.Slug != nil {
		slug = string(*body.Slug)
	}
	if slug == "" {
		slug = slugify(name)
	}
	if !validSlug(slug) {
		return errs.InvalidField("slug",
			"must be lowercase letters, digits and hyphens, and at most 63 characters")
	}

	// The id is generated HERE, before the insert, because the policy on `orgs`
	// compares the row's id to app.org_id — so the transaction has to be scoped
	// to the org being created. An unscoped insert is refused, and an insert
	// naming any other id is refused too, which means a request cannot create
	// an org it did not declare. See db/migrations/00016.
	orgID := uuid.New()
	out := apiv1.Org{
		Id:   common.Uuid(orgID.String()),
		Name: name,
		Slug: common.Slug(slug),
	}

	err = h.Pool.ScopeOrgAndUser(r.Context(), orgID.String(), userID.String(),
		func(ctx context.Context, tx pgx.Tx) error {
			var created time.Time
			if err := tx.QueryRow(ctx, `
				insert into orgs (id, name, slug) values ($1, $2, $3)
				returning created_at`, orgID, name, slug).Scan(&created); err != nil {
				return err
			}
			out.CreatedAt = common.Timestamp(created.UTC())

			if _, err := tx.Exec(ctx, `
				insert into org_members (org_id, user_id, role) values ($1, $2, 'owner')`,
				orgID, userID); err != nil {
				return err
			}
			// In the same transaction: an audit row that is missing because the
			// request failed after the commit looks like the org was never
			// created.
			return h.Audit.RecordTx(ctx, tx,
				audit.OrgCreate(h.actor(r, userID, orgID), slug, name))
		})
	if err != nil {
		if db.IsUniqueViolation(err) {
			return errs.Conflict("conflict", "That organisation name is already taken.").
				WithFix("Pick a different name, or set a slug explicitly.").
				WithDetail("field", "slug").
				WithCause(err)
		}
		return err
	}
	return httpx.JSON(w, http.StatusCreated, out)
}

// listOrgMembers implements GET /v1/orgs/{org}/members.
func (h *Handlers) listOrgMembers(w http.ResponseWriter, r *http.Request) error {
	m := tenancy.MustFrom(r.Context())

	members := []apiv1.OrgMember{}
	err := h.Tenancy.Scoped(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select u.id, u.email, u.name, om.role
			  from org_members om
			  join users u on u.id = om.user_id
			 where om.org_id = $1
			 order by om.role, u.email`, m.OrgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var om apiv1.OrgMember
			var id, email, role string
			var name *string
			if err := rows.Scan(&id, &email, &name, &role); err != nil {
				return err
			}
			om.UserId = common.Uuid(id)
			om.Email = openapi_types.Email(email)
			om.Name = name
			om.Role = common.Role(role)
			members = append(members, om)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}

	// The whole member list, unpaginated, and `page` says so. An org's
	// membership is tens of rows, not thousands, and a cursor nobody needs is a
	// cursor that goes untested until the first org that does need one.
	return httpx.JSON(w, http.StatusOK, struct {
		Members []apiv1.OrgMember `json:"members"`
		Page    common.Page       `json:"page"`
	}{Members: members, Page: common.Page{HasMore: false}})
}

// slugify derives a DNS-label-safe slug from a display name.
//
// Lossy on purpose: it is a suggestion, and the caller can always send one
// explicitly. What it must not do is produce something invalid — a slug reaches
// a preview hostname (SPEC §9), so an empty or over-long result is refused by
// validSlug rather than silently truncated into someone else's name.
var (
	nonSlug     = regexp.MustCompile(`[^a-z0-9]+`)
	slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

func slugify(name string) string {
	s := nonSlug.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, "-")
	if len(s) > 63 {
		s = strings.Trim(s[:63], "-")
	}
	return s
}

func validSlug(s string) bool { return slugPattern.MatchString(s) }
