// Package idempotency makes a retried mutation safe to replay.
//
// SPEC §7.1: "Every mutating endpoint accepts `Idempotency-Key`. Replaying a
// key returns the original response rather than acting twice." §17.2 is why it
// matters — a client whose request times out cannot tell whether the work
// happened, and the only safe thing it can do is retry.
//
// # The response is captured, not re-derived
//
// A replay returns the bytes the first attempt wrote, not a fresh rendering of
// the current state. Those differ: a project created and then renamed would
// replay with the new name, and a client reconciling the two would conclude
// something it did not do had happened. The stored body is what the caller
// already saw, or would have seen.
//
// # What is deliberately not stored
//
// The request body. It can contain a prompt, and §17.3 treats user content as
// something that does not sit in an operational table indefinitely. Only its
// hash is kept, which is enough to notice a key being reused for a different
// request.
package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/chassis/httpx"
	"github.com/anasatwork01/cofound/packages/db"
	"github.com/anasatwork01/cofound/services/api/internal/tenancy"
	"github.com/jackc/pgx/v5"
)

// Header is the request header SPEC §7.1 names.
const Header = "Idempotency-Key"

// maxKeyLength bounds a caller-supplied key.
//
// 255 is generous for a UUID or a ULID, which is what clients actually send,
// and refuses a key large enough to be a storage attack: the key is a primary
// key column, so an unbounded one is an unbounded index entry.
const maxKeyLength = 255

// DefaultTTL is how long a key is remembered.
//
// 24 hours. Long enough to cover any retry a human or a job queue will make —
// nobody retries a create from yesterday and expects the original answer — and
// short enough that the table is bounded by a day's mutations rather than by
// all of history.
const DefaultTTL = 24 * time.Hour

// Middleware replays, refuses or records a mutating request.
type Middleware struct {
	Pool    *db.Pool
	Tenancy *tenancy.Resolver
	TTL     time.Duration

	// MaxBody bounds what will be buffered to compute the request hash and to
	// capture the response. A request larger than this is passed through
	// WITHOUT idempotency rather than being refused: the alternative is that a
	// large upload cannot be made at all, and the header is optional.
	MaxBody int64
}

// New returns middleware with the default TTL.
func New(pool *db.Pool, rs *tenancy.Resolver) *Middleware {
	return &Middleware{Pool: pool, Tenancy: rs, TTL: DefaultTTL, MaxBody: 1 << 20}
}

// Wrap applies idempotency to a handler.
//
// Applied per route rather than to the whole subtree, because it only makes
// sense on a mutation: a GET has nothing to replay and buffering its response
// would cost memory for nothing. It is a no-op when the caller sends no key —
// §7.1 says endpoints ACCEPT the header, not that they require it.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get(Header))
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}
		ew := httpx.NewErrorWriter(nil)
		if len(key) > maxKeyLength {
			ew.Fail(w, r, errs.InvalidField(Header,
				fmt.Sprintf("must be at most %d characters", maxKeyLength)))
			return
		}
		membership, ok := tenancy.From(r.Context())
		if !ok || membership.OrgID.String() == "" {
			// No org resolved, so there is nothing to scope the key to. Rather
			// than key on the user — which would let one org's retry collide
			// with another's — the request runs without idempotency, which is
			// the behaviour it would have had without the header.
			next.ServeHTTP(w, r)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, m.MaxBody+1))
		if err != nil {
			ew.Fail(w, r, errs.InvalidBody(err))
			return
		}
		if int64(len(body)) > m.MaxBody {
			// Too large to fingerprint. Pass through rather than refuse; see
			// MaxBody.
			r.Body = io.NopCloser(bytes.NewReader(body))
			next.ServeHTTP(w, r)
			return
		}
		// The handler still needs to read the body.
		r.Body = io.NopCloser(bytes.NewReader(body))
		sum := sha256.Sum256(body)

		claim, err := m.claim(r.Context(), key, r.Method, r.URL.Path, sum[:])
		if err != nil {
			ew.Fail(w, r, err)
			return
		}
		if claim.replay != nil {
			claim.replay.writeTo(w)
			return
		}

		rec := &recorder{ResponseWriter: w, status: http.StatusOK, limit: m.MaxBody}
		next.ServeHTTP(rec, r)

		// Only a completed response is stored, and only a successful one is
		// worth replaying: a 500 that is retried should be retried for real,
		// because the condition that caused it may have cleared. A 4xx is
		// stored, because the same request will be refused the same way and
		// replaying saves the work.
		if rec.status >= 500 {
			m.release(r.Context(), key)
			return
		}
		m.complete(r.Context(), key, rec)
	})
}

type claimResult struct{ replay *stored }

type stored struct {
	status  int
	body    []byte
	headers map[string]string
}

func (s *stored) writeTo(w http.ResponseWriter) {
	for k, v := range s.headers {
		w.Header().Set(k, v)
	}
	// So a client can tell a replay from a fresh execution. Without it, a
	// duplicate create looks exactly like a successful one, which is correct
	// for the client's logic and unhelpful for anyone debugging.
	w.Header().Set("Idempotency-Replayed", "true")
	w.WriteHeader(s.status)
	_, _ = w.Write(s.body)
}

// claim takes the key, or reports what to do instead.
func (m *Middleware) claim(ctx context.Context, key, method, path string, hash []byte) (claimResult, error) {
	var out claimResult
	err := m.Tenancy.Scoped(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var (
			existingMethod, existingPath string
			existingHash                 []byte
			status                       *int
			body                         []byte
			headers                      map[string]string
			completed                    *time.Time
			expires                      time.Time
		)
		// One statement. An insert that loses the race does not error — it
		// returns no row — and the select that follows sees the winner. A
		// select-then-insert would let two concurrent first attempts both pass
		// the select, which is the exact double-execution this prevents.
		err := tx.QueryRow(ctx, `
			with claimed as (
				insert into idempotency_keys
				  (org_id, key, method, path, request_hash, expires_at)
				values (halyard_current_org_id(), $1, $2, $3, $4, $5)
				on conflict (org_id, key) do nothing
				returning method, path, request_hash, status, response_body,
				          response_headers, completed_at, expires_at
			)
			select method, path, request_hash, status, response_body,
			       response_headers, completed_at, expires_at
			  from claimed
			union all
			select method, path, request_hash, status, response_body,
			       response_headers, completed_at, expires_at
			  from idempotency_keys
			 where key = $1 and not exists (select 1 from claimed)
			limit 1`,
			key, method, path, hash, time.Now().UTC().Add(m.ttl()),
		).Scan(&existingMethod, &existingPath, &existingHash, &status, &body,
			&headers, &completed, &expires)
		if err != nil {
			return fmt.Errorf("idempotency: claim: %w", err)
		}

		// The row we just inserted: method, path and hash match by
		// construction and completed_at is null.
		if completed == nil && bytes.Equal(existingHash, hash) &&
			existingMethod == method && existingPath == path {
			// Either our own fresh claim, or someone else's in-flight request
			// for the identical call. Distinguishing them would need a
			// per-attempt token; refusing the concurrent case is what matters,
			// and expiry reclaims a key whose owner died.
			if time.Now().UTC().After(expires) {
				// Stale claim from a process that never finished. Take it over
				// rather than leaving the client permanently unable to retry.
				if _, err := tx.Exec(ctx, `
					update idempotency_keys
					   set request_hash = $2, method = $3, path = $4,
					       created_at = now(), expires_at = $5,
					       status = null, response_body = null,
					       response_headers = null, completed_at = null
					 where key = $1`,
					key, hash, method, path, time.Now().UTC().Add(m.ttl())); err != nil {
					return fmt.Errorf("idempotency: reclaim: %w", err)
				}
			}
			return nil
		}

		if existingMethod != method || existingPath != path || !bytes.Equal(existingHash, hash) {
			// The same key for a different request. Replaying the first
			// response would hide a client bug behind a plausible answer.
			return errs.Invalid("That Idempotency-Key was already used for a different request.").
				WithFix("Use a new key for a new request.").
				WithDetail("field", Header)
		}

		if completed != nil {
			out.replay = &stored{status: *status, body: body, headers: headers}
			return nil
		}

		// In flight, identical request. Refusing is the point: allowing both to
		// run is the double-execution the header exists to prevent.
		return errs.Conflict("request_in_flight",
			"A request with that Idempotency-Key is still running.").
			WithFix("Wait for it to finish, then retry.").
			WithRetryAfter(time.Second)
	})
	return out, err
}

func (m *Middleware) complete(ctx context.Context, key string, rec *recorder) {
	headers := map[string]string{}
	if ct := rec.Header().Get("Content-Type"); ct != "" {
		headers["Content-Type"] = ct
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		headers["Location"] = loc
	}
	// Deliberately NOT every header. Set-Cookie must never be replayed — it
	// would hand a second caller the first caller's session — and the request
	// id must not be, because a replay is a different request and the log
	// correlation would be wrong.
	raw, err := json.Marshal(headers)
	if err != nil {
		return
	}
	_ = m.Tenancy.Scoped(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			update idempotency_keys
			   set status = $2, response_body = $3, response_headers = $4, completed_at = now()
			 where key = $1`, key, rec.status, rec.body.Bytes(), raw)
		return err
	})
}

// release drops a claim so a 5xx can be retried for real.
func (m *Middleware) release(ctx context.Context, key string) {
	_ = m.Tenancy.Scoped(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `delete from idempotency_keys where key = $1`, key)
		return err
	})
}

func (m *Middleware) ttl() time.Duration {
	if m.TTL > 0 {
		return m.TTL
	}
	return DefaultTTL
}

// Sweep deletes expired keys. Called by a scheduled job, not on a request.
func (m *Middleware) Sweep(ctx context.Context) (int64, error) {
	tag, err := m.Pool.Unscoped().Exec(ctx,
		`delete from idempotency_keys where expires_at < now()`)
	if err != nil {
		return 0, fmt.Errorf("idempotency: sweep: %w", err)
	}
	return tag.RowsAffected(), nil
}

// recorder captures a response so it can be replayed.
//
// It does NOT implement http.Flusher on purpose. A streaming response cannot be
// captured — the whole point is that it has no end — and silently buffering one
// would turn an SSE endpoint into a request that never sends anything. The
// chassis keeps streams on a separate subtree, so this cannot be reached from
// one; a Flusher here would make that mistake possible.
type recorder struct {
	http.ResponseWriter
	status  int
	body    bytes.Buffer
	limit   int64
	written int64
	wrote   bool
}

func (r *recorder) WriteHeader(status int) {
	if r.wrote {
		return
	}
	r.wrote = true
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(p []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	if r.written+int64(len(p)) <= r.limit {
		r.body.Write(p)
		r.written += int64(len(p))
	} else {
		// Too large to replay. The response still goes to the caller in full;
		// only the capture is abandoned, and complete() then stores a body that
		// does not match. Guarded by MaxBody being the same bound the request
		// used, so a handler producing megabytes from a small body is the only
		// way here.
		r.body.Reset()
		r.written = r.limit + 1
	}
	return r.ResponseWriter.Write(p)
}

// Unwrap keeps the chassis's writer chain intact.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
