package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrNoOrg is returned when Scope is called without an org.
var ErrNoOrg = errors.New("db: Scope requires an org id")

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
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: begin: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			// A rollback on an already-finished transaction is harmless; the
			// error is deliberately discarded because the caller's error, or the
			// panic in flight, is the one that matters.
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	// set_config with is_local => true rather than `SET LOCAL`, for two reasons.
	// SET LOCAL cannot take a parameter — verified: `set local app.org_id = $1`
	// is a syntax error — so it would have to be built by string concatenation,
	// which is a SQL injection in the one place that must not have one. And
	// set_config's third argument makes the transaction scope explicit at the
	// call site rather than implied by a keyword.
	if _, err := tx.Exec(ctx, "select set_config('app.org_id', $1, true)", orgID); err != nil {
		return fmt.Errorf("db: scope to org: %w", err)
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

// ScopedOrg reports the org the current transaction is scoped to. For tests and
// for the assertion in a query helper that wants to be sure.
func ScopedOrg(ctx context.Context, tx pgx.Tx) (string, error) {
	var org string
	err := tx.QueryRow(ctx,
		`select coalesce(nullif(current_setting('app.org_id', true), ''), '')`).Scan(&org)
	return org, err
}
