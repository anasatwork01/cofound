// Command aigw is a placeholder. AI gateway - LLM provider proxy, token metering, spend caps, caching
//
// Implemented in a later task; see docs/TASKS.md.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
)

const service = "aigw"

// Build metadata, injected with -ldflags at build time. See the Makefile.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	printVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *printVersion {
		fmt.Printf("%s %s (%s)\n", service, version, commit)
		return
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	log.Info("scaffolded, not yet implemented",
		"service", service,
		"version", version,
		"commit", commit,
	)
}
