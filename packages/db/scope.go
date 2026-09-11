package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrNoOrg is returned when Scope is called without an org.
var ErrNoOrg = errors.New("db: Scope requires an org id")

// ErrNoUser is returned when ScopeUser is called without a user.
var ErrNoUser = errors.New("db: ScopeUser requires a user id")

// Principal is who a scoped transaction acts as.
//
// Both fields are optional independently, because the two questions they answer
// arrive at different times. The user is known as soon as the session cookie is
// verified; the org is not known until the route is matched. Migration 00016
// exists because two tables — `orgs` and `org_members` — must be readable in
// the window where the user is known and the org is not.
type Principal struct {
	OrgID  string
	UserID string
}

// ScopeAs runs fn inside a transaction scoped to p.
//
// The general form. Scope and ScopeUser are the two shapes callers actually
// want and are thin wrappers over this.
func (p *Pool) ScopeAs(ctx context.Context, who Principal, fn func(context.Context, pgx.Tx) error) error {
	if who.OrgID == "" && who.UserID == "" {
		return ErrNoOrg
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	// set_config with is_local => true, never `SET LOCAL`. SET LOCAL cannot
	// take a parameter — verified: `set local app.org_id = $1` is a syntax
	// error — so it would have to be built by string concatenation, which is a
	// SQL injection in the one place that must not have one.
	if who.OrgID != "" {
		if _, err := tx.Exec(ctx, "select set_config('app.org_id', $1, true)", who.OrgID); err != nil {
			return fmt.Errorf("db: scope to org: %w", err)
		}
	}
	if who.UserID != "" {
		if _, err := tx.Exec(ctx, "select set_config('app.user_id', $1, true)", who.UserID); err != nil {
			return fmt.Errorf("db: scope to user: %w", err)
		}
	}

	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit: %w", err)
	}
	committed = true
	return nil
}

// ScopeUser runs fn inside a transaction scoped to a user but NOT to an org.
//
// For the window where the caller is known and the org is not: resolving which
// org a slug names, and listing the orgs a user belongs to. Only `orgs` and
// `org_members` are readable in this state — every other tenant table's policy
// compares against app.org_id, which is unset, so it matches nothing. That is
// the correct failure: a query that needs an org must say which.
func (p *Pool) ScopeUser(ctx context.Context, userID string, fn func(context.Context, pgx.Tx) error) error {
	if userID == "" {
		return ErrNoUser
	}
	return p.ScopeAs(ctx, Principal{UserID: userID}, fn)
}

// Scope runs fn inside a transaction whose `app.org_id` is orgID, so that every
// row-level security policy applies.
//
// The transaction is not an implementation detail. `app.org_id` set outside one
// persists on the pooled connection and is inherited by whoever borrows it next
// — verified — which would show one tenant another tenant's data with no code
// change and no error. So there is no exported way to set the org without also
// opening a transaction, and none to keep the transaction open past fn.
//
// fn is rolled back on error and on panic; a panic is re-raised after the
// rollback so the chassis's recovery middleware still sees it.
func (p *Pool) Scope(ctx context.Context, orgID string, fn func(context.Context, pgx.Tx) error) error {
	if orgID == "" {
		// An empty org would set app.org_id to '', which the policies read as
		// NULL and match zero rows against. That is the correct failure, but it
		// surfaces as a mysteriously empty result set rather than as a bug, so
		// it is refused here instead.
		return ErrNoOrg
	}
	return p.ScopeAs(ctx, Principal{OrgID: orgID}, fn)
}

// ScopeOrgAndUser scopes to both, which is what a request past tenancy
// resolution should use: `orgs` and `org_members` then admit the caller's own
// rows as well as the current org's.
func (p *Pool) ScopeOrgAndUser(ctx context.Context, orgID, userID string, fn func(context.Context, pgx.Tx) error) error {
	if orgID == "" {
		return ErrNoOrg
	}
	return p.ScopeAs(ctx, Principal{OrgID: orgID, UserID: userID}, fn)
}

// ScopedOrg reports the org the current transaction is scoped to. For tests and
// for the assertion in a query helper that wants to be sure.
func ScopedOrg(ctx context.Context, tx pgx.Tx) (string, error) {
	var org string
	err := tx.QueryRow(ctx,
		`select coalesce(nullif(current_setting('app.org_id', true), ''), '')`).Scan(&org)
	return org, err
}
