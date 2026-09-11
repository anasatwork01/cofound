package v1

import (
	"net"
	"net/http"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/httpx"
	"github.com/anasatwork01/cofound/packages/db"
	"github.com/anasatwork01/cofound/services/api/internal/audit"
	"github.com/anasatwork01/cofound/services/api/internal/tenancy"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Handlers is the /v1 surface.
type Handlers struct {
	Pool    *db.Pool
	Tenancy *tenancy.Resolver
	Audit   *audit.Writer

	// Idempotent wraps every mutating route. SPEC §7.1: "Every mutating
	// endpoint accepts Idempotency-Key."
	Idempotent func(http.Handler) http.Handler

	// InviteTTL is how long an invitation works. Long enough to survive a
	// weekend and a spam folder; short enough that a stale link in an inbox is
	// not a standing offer of access to an org.
	InviteTTL time.Duration
}

// DefaultInviteTTL is seven days.
func DefaultInviteTTL() time.Duration { return 7 * 24 * time.Hour }

// Mount registers the routes on the authed subtree.
//
// Every route carries a tenancy.Require, and that is the whole of SPEC §8's
// "role checks in a single middleware, never inline": the permission is
// declared beside the route, and no handler below consults the matrix itself.
// A reviewer can read the required role off this function.
func (h *Handlers) Mount(r chi.Router, ew *httpx.ErrorWriter) {
	rs := h.Tenancy
	if h.InviteTTL == 0 {
		h.InviteTTL = DefaultInviteTTL()
	}

	// Applied per route rather than to the subtree, because it only makes sense
	// on a mutation: a GET has nothing to replay, and buffering its response
	// would cost memory for nothing.
	idem := h.Idempotent
	if idem == nil {
		idem = func(next http.Handler) http.Handler { return next }
	}

	r.Route("/v1", func(r chi.Router) {
		// Creating an org names no org, so there is no membership to check —
		// anyone signed in may create one. It is the one authenticated route
		// with no Require, and it reads the caller from tenancy.Caller.
		r.With(idem).Post("/orgs", ew.H(h.createOrg))

		r.Route("/orgs/{org}", func(r chi.Router) {
			r.With(rs.Require(tenancy.ActionOrgRead)).Get("/members", ew.H(h.listOrgMembers))
			r.With(rs.Require(tenancy.ActionMemberInvite), idem).Post("/invites", ew.H(h.createInvite))
			r.With(rs.Require(tenancy.ActionMemberRemove), idem).
				Delete("/members/{user}", ew.H(h.removeMember))
			r.With(rs.Require(tenancy.ActionMemberRoleChange), idem).
				Patch("/members/{user}", ew.H(h.changeMemberRole))
		})

		// Accepting an invitation names no org either: the invitee is not a
		// member yet, so there is nothing to check a role against. The token
		// is the authorisation.
		r.With(idem).Post("/invites/accept", ew.H(h.acceptInvite))

		r.With(rs.Require(tenancy.ActionProjectCreate), idem).Post("/projects", ew.H(h.createProject))
		r.With(rs.Require(tenancy.ActionProjectRead)).Get("/projects", ew.H(h.listProjects))
		r.Route("/projects/{project}", func(r chi.Router) {
			r.With(rs.Require(tenancy.ActionProjectRead)).Get("/", ew.H(h.getProject))
			r.With(rs.Require(tenancy.ActionProjectDelete), idem).Delete("/", ew.H(h.archiveProject))
		})
	})
}

// actor builds the common half of an audit entry.
func (h *Handlers) actor(r *http.Request, userID, orgID uuid.UUID) audit.Actor {
	return audit.Actor{
		UserID:    userID,
		OrgID:     orgID,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	}
}

// actorFor builds it from the resolved membership.
func (h *Handlers) actorFor(r *http.Request) audit.Actor {
	m := tenancy.MustFrom(r.Context())
	return h.actor(r, m.UserID, m.OrgID)
}

// clientIP reads the peer the chassis resolved.
//
// The chassis's ClientIP middleware already applied the TRUSTED_PROXY_CIDRS
// policy, so this does not re-parse X-Forwarded-For: doing so would trust a
// header the chassis deliberately does not.
func clientIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}
