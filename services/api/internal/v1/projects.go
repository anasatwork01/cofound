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
	"github.com/anasatwork01/cofound/services/api/internal/tenancy"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// createProject implements POST /v1/projects.
//
// §7.1 takes either a template version or a prompt, never both: with a prompt
// the agent picks the template itself. The SLO is p50 < 10s to a first preview
// (§17.3, provisional until task 1.19 measures Modal's snapshot and restore
// latency), which is why this returns as soon as the project and branch rows
// exist rather than waiting for a sandbox — acquiring one is task 1.3's job,
// and doing it here would put a Modal round trip inside the create.
func (h *Handlers) createProject(w http.ResponseWriter, r *http.Request) error {
	m := tenancy.MustFrom(r.Context())

	// The request body names the org, and the tenancy middleware resolved one
	// from the X-Halyard-Org header. They must agree: honouring the body over
	// the resolved scope would let a caller create a project in an org whose
	// role was never checked.
	raw, err := httpx.Decode[struct {
		OrgID             string  `json:"org_id"`
		Name              string  `json:"name"`
		TemplateVersionID *string `json:"template_version_id"`
		Prompt            *string `json:"prompt"`
	}](r, maxBody)
	if err != nil {
		return err
	}

	bodyOrg, err := uuid.Parse(raw.OrgID)
	if err != nil {
		return errs.InvalidField("org_id", "must be a uuid")
	}
	if bodyOrg != m.OrgID {
		// Reported as not-found rather than forbidden: the caller may not be a
		// member of the org they named, and saying "you may not use that org"
		// would confirm it exists.
		return errs.NotFound("org", "")
	}

	hasTemplate := raw.TemplateVersionID != nil && *raw.TemplateVersionID != ""
	hasPrompt := raw.Prompt != nil && strings.TrimSpace(*raw.Prompt) != ""
	switch {
	case hasTemplate == hasPrompt:
		// Both or neither. The schema says oneOf, and a request with both is
		// ambiguous about which the agent should honour.
		return errs.Invalid("Send either a template version or a prompt, not both.").
			WithFix("Pick a template, or describe what you want built.")
	case hasPrompt && raw.Name == "":
		// §7.1's prompt variant does not require a name; the agent names it.
		// Until task 1.x does, a placeholder beats an empty string in the UI.
		raw.Name = "Untitled project"
	}

	name := strings.TrimSpace(raw.Name)
	if name == "" {
		return errs.InvalidField("name", "is required")
	}
	slug := slugify(name)
	if !validSlug(slug) {
		return errs.InvalidField("name",
			"must contain letters or digits so a project slug can be derived from it")
	}

	var templateVersion *uuid.UUID
	if hasTemplate {
		id, err := uuid.Parse(*raw.TemplateVersionID)
		if err != nil {
			return errs.InvalidField("template_version_id", "must be a uuid")
		}
		templateVersion = &id
	}

	out := apiv1.Project{
		OrgId:        common.Uuid(m.OrgID.String()),
		Name:         name,
		GitAuthority: apiv1.Internal,
	}
	err = h.Tenancy.Scoped(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		if templateVersion != nil {
			// Verified rather than trusted: template_versions is a global
			// catalogue with no policy, so a bad id would otherwise become a
			// foreign key error rendered as an internal fault.
			var ready bool
			if err := tx.QueryRow(ctx,
				`select image_ready from template_versions where id = $1`,
				*templateVersion).Scan(&ready); err != nil {
				if errors.Is(err, db.ErrNoRows) {
					return errs.InvalidField("template_version_id", "does not name a known template version")
				}
				return err
			}
		}

		var (
			id        uuid.UUID
			created   time.Time
			branchID  uuid.UUID
			defBranch string
		)
		err := tx.QueryRow(ctx, `
			insert into projects (org_id, name, slug, template_version_id)
			values ($1, $2, $3, $4)
			returning id, created_at, default_branch`,
			m.OrgID, name, slug, templateVersion).Scan(&id, &created, &defBranch)
		if err != nil {
			return err
		}
		// The default branch row exists from the start. A project with no
		// branch is a project the session endpoints cannot open, and creating
		// it lazily means every caller has to handle the gap.
		if err := tx.QueryRow(ctx, `
			insert into branches (project_id, org_id, name) values ($1, $2, $3)
			returning id`, id, m.OrgID, defBranch).Scan(&branchID); err != nil {
			return err
		}

		out.Id = common.Uuid(id.String())
		out.Slug = common.Slug(slug)
		out.DefaultBranch = defBranch
		out.CreatedAt = common.Timestamp(created.UTC())
		out.Branches = &[]apiv1.Branch{{Id: common.Uuid(branchID.String()), Name: defBranch}}

		return h.Audit.RecordTx(ctx, tx, audit.ProjectCreate(h.actorFor(r), id, slug, name))
	})
	if err != nil {
		if db.IsUniqueViolation(err) {
			return errs.Conflict("conflict", "That project name is already taken in this organisation.").
				WithFix("Pick a different name.").
				WithDetail("field", "name").
				WithCause(err)
		}
		return err
	}
	return httpx.JSON(w, http.StatusCreated, out)
}

// listProjects implements GET /v1/projects.
//
// Not in §7.1's list, which jumps from POST /v1/projects to GET
// /v1/projects/:p. The console cannot render a project picker without it, and
// §8's org switching lives in that picker.
func (h *Handlers) listProjects(w http.ResponseWriter, r *http.Request) error {
	projects := []apiv1.Project{}
	err := h.Tenancy.Scoped(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		// No org predicate: row-level security supplies it, and §6 asks for
		// explicit filtering too — which the scope IS here, since the org comes
		// from the resolved membership rather than from the request.
		rows, err := tx.Query(ctx, `
			select id, org_id, name, slug, default_branch, git_authority, archived_at, created_at
			  from projects
			 where archived_at is null
			 order by created_at desc`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scanProject(rows)
			if err != nil {
				return err
			}
			projects = append(projects, p)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, struct {
		Projects []apiv1.Project `json:"projects"`
		Page     common.Page     `json:"page"`
	}{Projects: projects, Page: common.Page{HasMore: false}})
}

// getProject implements GET /v1/projects/{project}.
func (h *Handlers) getProject(w http.ResponseWriter, r *http.Request) error {
	m := tenancy.MustFrom(r.Context())

	var out apiv1.Project
	err := h.Tenancy.Scoped(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			select id, org_id, name, slug, default_branch, git_authority, archived_at, created_at
			  from projects where id = $1`, m.ProjectID)
		p, err := scanProject(row)
		if err != nil {
			if errors.Is(err, db.ErrNoRows) {
				return errs.NotFound("project", "")
			}
			return err
		}
		out = p

		rows, err := tx.Query(ctx,
			`select id, name, head_sha from branches where project_id = $1 order by name`,
			m.ProjectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		branches := []apiv1.Branch{}
		for rows.Next() {
			var b apiv1.Branch
			var id, name string
			var head *string
			if err := rows.Scan(&id, &name, &head); err != nil {
				return err
			}
			b.Id, b.Name = common.Uuid(id), name
			if head != nil {
				sha := common.GitSha(*head)
				b.HeadSha = &sha
			}
			branches = append(branches, b)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		out.Branches = &branches
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, out)
}

// archiveProject implements DELETE /v1/projects/{project}.
//
// Archives rather than erasing. §19.3 asks how long a suspended project's data
// is kept and how the user is warned before deletion, so erasure is a separate
// retention job — and "delete" that cannot be undone in the first minute after
// a misclick is a support burden nobody needs.
func (h *Handlers) archiveProject(w http.ResponseWriter, r *http.Request) error {
	m := tenancy.MustFrom(r.Context())

	err := h.Tenancy.Scoped(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var slug string
		err := tx.QueryRow(ctx, `
			update projects set archived_at = now()
			 where id = $1 and archived_at is null
			returning slug`, m.ProjectID).Scan(&slug)
		if err != nil {
			if errors.Is(err, db.ErrNoRows) {
				// Already archived, or gone. Idempotent: a second delete is not
				// an error, and the caller's intent is satisfied either way.
				return nil
			}
			return err
		}
		return h.Audit.RecordTx(ctx, tx, audit.ProjectArchive(h.actorFor(r), m.ProjectID, slug))
	})
	if err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// scanner is what both a Row and a Rows satisfy.
type scanner interface{ Scan(...any) error }

func scanProject(s scanner) (apiv1.Project, error) {
	var (
		p                                        apiv1.Project
		id, orgID, name, slug, branch, authority string
		archived                                 *time.Time
		created                                  time.Time
	)
	if err := s.Scan(&id, &orgID, &name, &slug, &branch, &authority, &archived, &created); err != nil {
		return p, err
	}
	p.Id = common.Uuid(id)
	p.OrgId = common.Uuid(orgID)
	p.Name = name
	p.Slug = common.Slug(slug)
	p.DefaultBranch = branch
	p.GitAuthority = apiv1.GitAuthority(authority)
	p.CreatedAt = common.Timestamp(created.UTC())
	if archived != nil {
		ts := common.Timestamp(archived.UTC())
		p.ArchivedAt = &ts
	}
	return p, nil
}
