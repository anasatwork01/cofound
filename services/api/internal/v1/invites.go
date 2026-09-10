package v1

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/chassis/httpx"
	"github.com/anasatwork01/cofound/packages/db"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/apiv1"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/common"
	"github.com/anasatwork01/cofound/services/api/internal/audit"
	"github.com/anasatwork01/cofound/services/api/internal/auth"
	"github.com/anasatwork01/cofound/services/api/internal/tenancy"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// createInvite implements POST /v1/orgs/{org}/invites.
//
// Requires admin (the matrix; §8 gives member management to admin). The
// response deliberately does NOT contain the token: it goes to the invitee's
// address, and a token in a response body is a token in whatever logged that
// response. The Invite schema has no field for one.
func (h *Handlers) createInvite(w http.ResponseWriter, r *http.Request) error {
	m := tenancy.MustFrom(r.Context())
	body, err := httpx.Decode[apiv1.CreateInviteRequest](r, maxBody)
	if err != nil {
		return err
	}

	email, err := auth.NormaliseEmail(string(body.Email))
	if err != nil {
		return err
	}
	role := tenancy.Role(body.Role)
	if !role.Valid() {
		return errs.InvalidField("role", "must be owner, admin, editor or viewer")
	}
	// An admin cannot invite an owner. Otherwise "everything except billing and
	// delete" (§8) includes manufacturing someone who can do both, which makes
	// the carve-out decorative.
	if role == tenancy.RoleOwner && m.Role != tenancy.RoleOwner {
		return errs.Forbidden("invite an owner").
			WithFix("Ask an owner to send this invitation.").
			WithDetail("required_role", string(tenancy.RoleOwner)).
			WithDetail("your_role", string(m.Role))
	}

	token, hash, err := auth.NewToken()
	if err != nil {
		return err
	}
	_ = token // sent by email once Q5 is answered; see below.

	out := apiv1.Invite{
		Email: openapi_types.Email(email),
		Role:  common.Role(role),
	}
	err = h.Tenancy.Scoped(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var id uuid.UUID
		var expires time.Time
		err := tx.QueryRow(ctx, `
			insert into invites (org_id, email, role, token_hash, invited_by, expires_at)
			values ($1, $2, $3, $4, $5, $6)
			returning id, expires_at`,
			m.OrgID, email, string(role), []byte(hash), m.UserID,
			time.Now().UTC().Add(h.InviteTTL)).Scan(&id, &expires)
		if err != nil {
			return err
		}
		out.Id = common.Uuid(id.String())
		out.ExpiresAt = common.Timestamp(expires.UTC())
		return h.Audit.RecordTx(ctx, tx,
			audit.MemberInvite(h.actorFor(r), id, email, string(role)))
	})
	if err != nil {
		if db.IsUniqueViolation(err) {
			// The partial unique index allows one LIVE invite per address per
			// org. Reporting the conflict rather than silently replacing means
			// the admin knows a link is already outstanding.
			return errs.Conflict("conflict", "That address already has an open invitation.").
				WithFix("Revoke the existing invitation first, or wait for it to expire.").
				WithCause(err)
		}
		return err
	}

	// The email is not sent, and that is Q5 rather than an oversight: SPEC §8
	// requires email invitations but no transactional email provider is
	// specified anywhere. The invite row and its token exist, so the flow works
	// end to end as soon as a mailer does — and the token is deliberately not
	// returned here, so nothing downstream can start depending on reading it
	// out of the response.
	return httpx.JSON(w, http.StatusCreated, out)
}

// acceptInvite implements POST /v1/invites/accept.
//
// Not in SPEC §7.1's list, and necessary: §8 requires invitations, and an
// invitation nobody can accept is not one. The token IS the authorisation, so
// this route has no Require — the invitee is not a member yet, which is the
// whole point.
func (h *Handlers) acceptInvite(w http.ResponseWriter, r *http.Request) error {
	userID, ok := tenancy.Caller(r.Context())
	if !ok {
		return errs.Unauthenticated()
	}
	body, err := httpx.Decode[struct {
		Token string `json:"token"`
	}](r, maxBody)
	if err != nil {
		return err
	}
	token, ok := auth.ParseToken(strings.TrimSpace(body.Token))
	if !ok {
		return errs.Unauthenticated().
			WithMessage("That invitation link is no longer valid.").
			WithFix("Ask for a new invitation.")
	}
	hash := auth.HashToken(token)

	// The invite is looked up UNSCOPED: the accepting user is not a member of
	// the org yet, so no scope of theirs could see the row. The token is the
	// authorisation, and it is matched by hash — which is why only the hash is
	// stored.
	var (
		inviteID uuid.UUID
		orgID    uuid.UUID
		role     string
		email    string
	)
	err = h.Pool.Unscoped().QueryRow(r.Context(), `
		select id, org_id, role, email from invites
		 where token_hash = $1
		   and accepted_at is null
		   and revoked_at is null
		   and expires_at > now()`, []byte(hash)).Scan(&inviteID, &orgID, &role, &email)
	if err != nil {
		if errors.Is(err, db.ErrNoRows) {
			// Unknown, spent, revoked and expired are one answer, so the
			// endpoint cannot be used to learn which a captured token was.
			return errs.Unauthenticated().
				WithMessage("That invitation link is no longer valid.").
				WithFix("Ask for a new invitation.")
		}
		return err
	}

	err = h.Pool.ScopeOrgAndUser(r.Context(), orgID.String(), userID.String(),
		func(ctx context.Context, tx pgx.Tx) error {
			// Consume and join in one transaction, with `accepted_at is null`
			// still in the predicate: two concurrent clicks must not both
			// succeed, and the UPDATE is what makes the second find no row.
			var consumed int64
			tag, err := tx.Exec(ctx, `
				update invites set accepted_at = now(), accepted_by = $2
				 where id = $1 and accepted_at is null and revoked_at is null`, inviteID, userID)
			if err != nil {
				return err
			}
			consumed = tag.RowsAffected()
			if consumed == 0 {
				return errs.Unauthenticated().
					WithMessage("That invitation has already been used.")
			}

			// `do update` rather than `do nothing`: an existing member
			// accepting an invitation to a different role should get it, which
			// is how an admin promotes someone who is already in the org.
			if _, err := tx.Exec(ctx, `
				insert into org_members (org_id, user_id, role) values ($1, $2, $3)
				on conflict (org_id, user_id) do update set role = excluded.role`,
				orgID, userID, role); err != nil {
				return err
			}
			return h.Audit.RecordTx(ctx, tx,
				audit.InviteAccept(h.actor(r, userID, orgID), inviteID, role))
		})
	if err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// removeMember implements DELETE /v1/orgs/{org}/members/{user}.
//
// Not in §7.1's list; §8 names "member removal" as an audited action, which
// requires it to be possible.
func (h *Handlers) removeMember(w http.ResponseWriter, r *http.Request) error {
	m := tenancy.MustFrom(r.Context())
	subject, err := uuid.Parse(chi.URLParam(r, "user"))
	if err != nil {
		return errs.InvalidField("user", "must be a uuid")
	}

	err = h.Tenancy.Scoped(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx,
			`select role from org_members where org_id = $1 and user_id = $2`,
			m.OrgID, subject).Scan(&role); err != nil {
			if errors.Is(err, db.ErrNoRows) {
				return errs.NotFound("member", "")
			}
			return err
		}
		// An admin cannot remove an owner, for the same reason they cannot
		// invite one.
		if tenancy.Role(role) == tenancy.RoleOwner && m.Role != tenancy.RoleOwner {
			return errs.Forbidden("remove an owner").
				WithFix("Ask an owner to do this.")
		}
		// The last owner cannot be removed. An org with no owner has nobody who
		// can bill it, delete it or transfer it (§8) — it is unadministerable,
		// and the person clicking the button is usually not intending that.
		if tenancy.Role(role) == tenancy.RoleOwner {
			var owners int
			if err := tx.QueryRow(ctx,
				`select count(*) from org_members where org_id = $1 and role = 'owner'`,
				m.OrgID).Scan(&owners); err != nil {
				return err
			}
			if owners <= 1 {
				return errs.Conflict("last_owner",
					"An organisation must keep at least one owner.").
					WithFix("Make someone else an owner first, then remove this one.")
			}
		}
		if _, err := tx.Exec(ctx,
			`delete from org_members where org_id = $1 and user_id = $2`,
			m.OrgID, subject); err != nil {
			return err
		}
		return h.Audit.RecordTx(ctx, tx, audit.MemberRemove(h.actorFor(r), subject, role))
	})
	if err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// changeMemberRole implements PATCH /v1/orgs/{org}/members/{user}.
//
// §8 names "role change" as an audited action, so it has to exist.
func (h *Handlers) changeMemberRole(w http.ResponseWriter, r *http.Request) error {
	m := tenancy.MustFrom(r.Context())
	subject, err := uuid.Parse(chi.URLParam(r, "user"))
	if err != nil {
		return errs.InvalidField("user", "must be a uuid")
	}
	body, err := httpx.Decode[struct {
		Role string `json:"role"`
	}](r, maxBody)
	if err != nil {
		return err
	}
	next := tenancy.Role(body.Role)
	if !next.Valid() {
		return errs.InvalidField("role", "must be owner, admin, editor or viewer")
	}

	err = h.Tenancy.Scoped(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var current string
		if err := tx.QueryRow(ctx,
			`select role from org_members where org_id = $1 and user_id = $2`,
			m.OrgID, subject).Scan(&current); err != nil {
			if errors.Is(err, db.ErrNoRows) {
				return errs.NotFound("member", "")
			}
			return err
		}
		// Granting or revoking ownership is an owner's act. An admin who could
		// promote themselves would have "everything except billing and delete"
		// plus a way to get both.
		if m.Role != tenancy.RoleOwner &&
			(next == tenancy.RoleOwner || tenancy.Role(current) == tenancy.RoleOwner) {
			return errs.Forbidden("change an owner's role").
				WithFix("Ask an owner to do this.")
		}
		if tenancy.Role(current) == tenancy.RoleOwner && next != tenancy.RoleOwner {
			var owners int
			if err := tx.QueryRow(ctx,
				`select count(*) from org_members where org_id = $1 and role = 'owner'`,
				m.OrgID).Scan(&owners); err != nil {
				return err
			}
			if owners <= 1 {
				return errs.Conflict("last_owner",
					"An organisation must keep at least one owner.").
					WithFix("Make someone else an owner first.")
			}
		}
		if current == string(next) {
			// Idempotent: setting the role someone already has is not an error,
			// and writing an audit row saying nothing changed is noise.
			return nil
		}
		if _, err := tx.Exec(ctx,
			`update org_members set role = $3 where org_id = $1 and user_id = $2`,
			m.OrgID, subject, string(next)); err != nil {
			return err
		}
		return h.Audit.RecordTx(ctx, tx,
			audit.RoleChange(h.actorFor(r), subject, current, string(next)))
	})
	if err != nil {
		return err
	}
	return httpx.NoContent(w)
}
