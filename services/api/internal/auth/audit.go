package auth

import (
	"context"
	"fmt"
	"net"

	"github.com/anasatwork01/cofound/packages/db"
	"github.com/google/uuid"
)

// DBAudit writes sign-in events to audit_log.
//
// Only sign-in. SPEC §8 lists nine audited actions — sign-in, role change,
// secret read/write, proposal decision, publish, rollback, domain change,
// credit purchase and member removal — and task 0.9 owns the writer that
// covers all of them and the test that proves the list is complete. This is
// the first of the nine and it happens in this package, so it is written here
// rather than deferred: an audit trail with a gap at "sign-in" is not one.
type DBAudit struct{ Pool *db.Pool }

// ActionSignIn is the action name. A constant so task 0.9's completeness test
// can refer to the same string this writes.
const ActionSignIn = "sign_in"

// SignIn records a successful authentication.
//
// Written UNSCOPED and with a null org_id, because sign-in happens before any
// org is known — the user may belong to many (§8) and has not yet chosen. The
// audit_log columns are nullable for exactly this event.
func (a DBAudit) SignIn(ctx context.Context, userID uuid.UUID, method string, ip net.IP, userAgent string) error {
	if a.Pool == nil {
		return nil
	}
	_, err := a.Pool.Unscoped().Exec(ctx, `
		insert into audit_log (org_id, actor_user_id, actor_kind, action, target, ip, user_agent)
		values (null, $1, 'user', $2, jsonb_build_object('method', $3::text), $4, $5)`,
		userID, ActionSignIn, method, nullableIP(ip), truncate(userAgent, 512))
	if err != nil {
		return fmt.Errorf("auth: audit sign-in: %w", err)
	}
	return nil
}
