package db

import (
	"errors"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// NotFoundOnNoRows converts pgx's no-rows sentinel into a 404 and leaves
// anything else alone, so a handler's happy path stays free of error mapping.
//
// This lives here rather than in the chassis because the chassis has no
// database dependency -- deliberately, so that a service which does not talk to
// Postgres does not build pgx. errs.NotFoundOnNoRows matches on the message
// text for the same reason and says so; this is the precise version, and a
// service that imports this package should prefer it.
func NotFoundOnNoRows(err error, kind, ident string) *errs.Error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.NotFound(kind, ident).WithCause(err)
	}
	return nil
}

// Postgres error codes this package maps. Only the ones a handler should react
// to differently; everything else is an internal fault by default, which is the
// safe direction.
const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
	codeCheckViolation      = "23514"
	// Raised when a row-level security policy refuses a write. It reaching a
	// handler means the application tried to write outside its tenant, which is
	// a bug in the application rather than in the request.
	codeRLSViolation         = "42501"
	codeSerializationFailure = "40001"
	codeDeadlockDetected     = "40P01"
)

// Classify maps a Postgres error to a chassis error.
//
// The mapping is deliberately narrow. A unique violation on a slug is a 409 the
// user can act on; a check violation is a bug in the caller's validation, and
// telling the user "that request is not valid" is both true and all we can
// safely say. Everything unrecognised becomes an internal fault whose cause
// reaches only the log -- there is no path from a driver message to a response
// body, which is what stops a constraint name or a column value leaking.
//
// A serialisation failure and a deadlock are retriable, and saying so is the
// difference between a client that retries and one that shows a red banner.
func Classify(err error) *errs.Error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.NotFound("", "").WithCause(err)
	}

	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return nil
	}

	switch pg.Code {
	case codeUniqueViolation:
		return errs.Conflict("conflict", "That already exists.").
			WithFix("Pick a different name and try again.").
			WithCause(err)
	case codeForeignKeyViolation:
		return errs.Invalid("That refers to something that does not exist.").
			WithFix("Reload and try again.").
			WithCause(err)
	case codeCheckViolation:
		return errs.Invalid("That request is not valid.").
			WithFix("Check the fields and try again.").
			WithCause(err)
	case codeRLSViolation:
		// Not a 403. A 403 would tell the caller the resource exists in another
		// tenant, and the whole point of the isolation is that cross-tenant
		// reads are indistinguishable from absence.
		return errs.Internal(err)
	case codeSerializationFailure, codeDeadlockDetected:
		return errs.Chassis().New(errs.CodeConflict).
			WithMessage("That conflicted with another change in flight.").
			WithFix("Try again.").
			WithCause(err)
	}
	return nil
}

// IsUniqueViolation reports whether err is a duplicate-key error, for the
// callers that want to branch rather than render.
func IsUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == codeUniqueViolation
}

// ConstraintName returns the constraint a violation names, or "".
//
// For the log and for a handler that must distinguish two unique constraints on
// one table -- api.openapi.yaml has endpoints where "slug taken" and "hostname
// taken" are different messages. Never put this in a response body: a
// constraint name is internal schema detail.
func ConstraintName(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.ConstraintName
	}
	return ""
}
