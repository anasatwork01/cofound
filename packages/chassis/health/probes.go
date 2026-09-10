package health

import (
	"context"
	"fmt"
	"net/http"
)

// Pinger is satisfied by *pgxpool.Pool, which task 0.6 introduces.
type Pinger interface {
	Ping(ctx context.Context) error
}

// PingProbe checks a connection pool.
func PingProbe(name string, p Pinger, c Criticality) Probe {
	return Probe{Name: name, Criticality: c, Check: p.Ping}
}

// PingFunc checks anything.
func PingFunc(name string, fn func(context.Context) error, c Criticality) Probe {
	return Probe{Name: name, Criticality: c, Check: fn}
}

// HTTPProbe GETs a peer's /healthz and requires 200.
//
// NEVER point it at a peer's /readyz. Readiness cascades: if A is unready
// because B is unready, and B is unready because C is, one slow dependency
// takes the whole control plane out of rotation at once. A peer's availability
// belongs in a request's 503, not in this instance's readiness.
func HTTPProbe(name, url string, client *http.Client, c Criticality) Probe {
	if client == nil {
		client = http.DefaultClient
	}
	return Probe{
		Name:        name,
		Criticality: c,
		Check: func(ctx context.Context) error {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("%s returned %d", url, resp.StatusCode)
			}
			return nil
		},
	}
}
