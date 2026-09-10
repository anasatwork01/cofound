// Command api is the REST API, auth, org/project CRUD, SSE gateway and
// approval gates.
package main

import (
	"github.com/anasatwork01/cofound/packages/chassis"
	"github.com/anasatwork01/cofound/packages/chassis/telemetry/otlp"

	"github.com/anasatwork01/cofound/services/api/internal/httpapi"
)

const service = "api"

// Build metadata, injected with -ldflags at build time. See the Makefile.
//
// These stay in package main because the Makefile injects -X main.version and
// -X main.commit. A chassis cannot own them without changing the ldflags paths,
// and moving them would silently produce "dev"/"unknown" in every production
// log line with no build failure.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	api := httpapi.New()

	chassis.Main(chassis.Service{
		Name:    service,
		Version: version,
		Commit:  commit,
		Bind:    api.Bind,
		Setup:   api.Setup,
		Probes:  api.Probes,
		// This one line opts api's binary into the OTLP exporter's ~65 modules
		// and ~10MB. gitd, aigw and mcp choose for themselves.
		Exporter: otlp.Factory,
	})
}
