package chassis

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/health"
	"github.com/anasatwork01/cofound/packages/chassis/logkey"
)

// AdminServer is the loopback-only operator surface.
//
// Splitting it off gets the security benefit of a separate port without the
// deployment lock-in of putting HEALTH there: /healthz and /readyz stay on the
// main port because SPEC 22 item 2 (whether the container host routes one port
// or several) is still unverified. A single-port platform could not reach an
// admin-port probe at all, so main-port health is merely suboptimal on a
// multi-port host while admin-port health is broken on a single-port one. Only
// the things that must not be public live here.
type AdminServer struct {
	srv      *http.Server
	log      *slog.Logger
	addr     string
	resolved []slog.Attr
}

// NewAdminServer builds the admin listener.
func NewAdminServer(
	addr string,
	log *slog.Logger,
	level *slog.LevelVar,
	reg *health.Registry,
	resolved []slog.Attr,
) *AdminServer {
	a := &AdminServer{log: log, addr: addr, resolved: resolved}

	mux := http.NewServeMux()

	// Full readiness detail, including each probe's error string, which the
	// unauthenticated main-port /readyz must never carry.
	mux.HandleFunc("GET /debug/readyz", func(w http.ResponseWriter, r *http.Request) {
		writeSnapshot(w, reg.Snapshot())
	})
	mux.HandleFunc("POST /debug/readyz", func(w http.ResponseWriter, r *http.Request) {
		writeSnapshot(w, reg.Check(r.Context()))
	})

	mux.HandleFunc("GET /debug/loglevel", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"level": level.Level().String()})
	})
	mux.HandleFunc("PUT /debug/loglevel", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Level string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expect {\"level\":\"debug|info|warn|error\"}"})
			return
		}
		var lv slog.Level
		if err := lv.UnmarshalText([]byte(body.Level)); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expect debug, info, warn or error"})
			return
		}
		level.Set(lv)
		// Raising to debug starts logging user content, so it is a privacy
		// action and not a config tweak. The console-facing equivalent in a
		// later task must be owner/admin-only and write an audit_log row.
		log.LogAttrs(r.Context(), slog.LevelWarn, "log level changed via admin listener",
			slog.String("level", lv.String()))
		writeJSON(w, http.StatusOK, map[string]string{"level": lv.String()})
	})

	mux.HandleFunc("GET /debug/config", func(w http.ResponseWriter, r *http.Request) {
		out := map[string]any{}
		for _, attr := range a.resolved {
			out[attr.Key] = attr.Value.String()
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)

	a.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		// No WriteTimeout: a pprof profile legitimately takes 30 seconds.
		IdleTimeout: 60 * time.Second,
	}
	return a
}

// Addr returns the admin address.
func (a *AdminServer) Addr() string { return a.addr }

// Run serves until ctx is cancelled.
func (a *AdminServer) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", a.addr)
	if err != nil {
		return err
	}
	a.addr = ln.Addr().String()
	a.log.LogAttrs(ctx, slog.LevelInfo, "admin listening",
		slog.String(logkey.Addr, a.addr), slog.String(logkey.Component, "admin"))

	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = a.srv.Shutdown(sctx)
	}()

	if err := a.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func writeSnapshot(w http.ResponseWriter, snap health.Snapshot) {
	type probe struct {
		Name        string `json:"name"`
		State       string `json:"state"`
		Criticality string `json:"criticality"`
		LatencyMS   int64  `json:"latency_ms"`
		Since       string `json:"since,omitempty"`
		Consecutive int    `json:"consecutive"`
		Error       string `json:"error,omitempty"`
	}
	out := struct {
		State    string  `json:"state"`
		Draining bool    `json:"draining"`
		Probes   []probe `json:"probes"`
	}{State: string(snap.State), Draining: snap.Draining}

	for _, r := range snap.Results {
		p := probe{
			Name: r.Name, State: string(r.State), Criticality: r.Criticality.String(),
			LatencyMS: r.Latency.Milliseconds(), Consecutive: r.Consecutive,
		}
		if !r.Since.IsZero() {
			p.Since = r.Since.UTC().Format(time.RFC3339)
		}
		if r.Err != nil {
			p.Error = r.Err.Error()
		}
		out.Probes = append(out.Probes, p)
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
