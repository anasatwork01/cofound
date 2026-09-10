package httpx

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/chassis/health"
)

// This file owns the health HANDLERS while package health owns the STATE. That
// split is what breaks the import cycle: health cannot expose an
// httpx.HandlerFunc without importing httpx, which imports health.

// HealthInfo is the liveness body.
type HealthInfo struct {
	Status  string `json:"status"`
	Service string `json:"service,omitempty"`
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
}

// ReadyInfo is the readiness body.
//
// Failing carries probe NAMES only. /readyz is unauthenticated, and a probe's
// error string routinely contains a hostname, a port or a driver message. Full
// detail lives on the loopback admin listener.
type ReadyInfo struct {
	Status   string   `json:"status"`
	Draining bool     `json:"draining"`
	Failing  []string `json:"failing,omitempty"`
}

// MountHealth adds /healthz and /readyz.
//
// They are mounted on the root, before any auth or tenancy middleware, so a
// probe never needs a credential.
func MountHealth(r chi.Router, reg *health.Registry, info HealthInfo, detail bool) {
	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		// Liveness answers "is this process running", never "are its
		// dependencies up" — otherwise a database blip makes the orchestrator
		// restart every replica, which is the opposite of helpful.
		out := info
		out.Status = "ok"
		_ = JSON(w, http.StatusOK, out)
	})

	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		snap := reg.Snapshot()
		body := ReadyInfo{Draining: snap.Draining}
		if detail {
			body.Failing = snap.Failing()
		} else if snap.State != health.StateUp {
			body.Failing = snap.Failing()
		}

		if snap.State == health.StateUp {
			body.Status = "ready"
			_ = JSON(w, http.StatusOK, body)
			return
		}
		body.Status = "unready"
		// 503 with the standard envelope, so a client decoder handles it like
		// any other error.
		e := errs.NotReady(snap.Failing())
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(e.Status)
		out, err := jsonMarshal(struct {
			ReadyInfo
			Error any `json:"error"`
		}{ReadyInfo: body, Error: e.Wire("").Code})
		if err != nil {
			return
		}
		_, _ = w.Write(out)
	})
}
