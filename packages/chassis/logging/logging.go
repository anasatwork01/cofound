package logging

import (
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/anasatwork01/cofound/packages/chassis/logkey"
)

// Format selects the output encoding.
type Format string

const (
	FormatJSON Format = "json"
	FormatText Format = "text"
)

// Config describes the process logger.
type Config struct {
	Service    string
	Env        string
	Version    string
	Commit     string
	InstanceID string

	Level  slog.Level
	Format Format

	// AddSource costs roughly 380ns and 6 allocations per line. Off in
	// production by default; invaluable locally.
	AddSource bool

	// Out defaults to os.Stdout. Writing to stdout rather than slog's default
	// stderr is 12-factor, and it keeps one ordered stream for the collector.
	Out io.Writer
}

// NewHandler builds the production handler chain: a Guard wrapping the encoder.
//
// There is no exported path to a bare JSON handler. That is the point — the
// Guard is unbypassable by construction rather than by convention, so no future
// call site can "just use slog directly" and quietly lose redaction.
func NewHandler(cfg Config) (slog.Handler, *slog.LevelVar) {
	out := cfg.Out
	if out == nil {
		out = os.Stdout
	}

	level := new(slog.LevelVar)
	level.Set(cfg.Level)

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.AddSource,
		// ReplaceAttr is used ONLY for presentation concerns this package owns.
		// Never for security: see the package doc for why that cannot work.
		ReplaceAttr: presentation,
	}

	var enc slog.Handler
	if cfg.Format == FormatText {
		enc = slog.NewTextHandler(out, opts)
	} else {
		enc = slog.NewJSONHandler(out, opts)
	}

	guard := NewGuard(enc, level)

	attrs := []slog.Attr{}
	if cfg.Service != "" {
		attrs = append(attrs, slog.String(logkey.Service, cfg.Service))
	}
	if cfg.Env != "" {
		attrs = append(attrs, slog.String(logkey.Env, cfg.Env))
	}
	if cfg.Version != "" {
		attrs = append(attrs, slog.String(logkey.Version, cfg.Version))
	}
	if cfg.Commit != "" {
		attrs = append(attrs, slog.String(logkey.Commit, cfg.Commit))
	}
	if cfg.InstanceID != "" {
		attrs = append(attrs, slog.String(logkey.Instance, cfg.InstanceID))
	}
	if len(attrs) == 0 {
		return guard, level
	}
	return guard.WithAttrs(attrs), level
}

// New builds the logger and returns the level knob so an incident can raise
// verbosity without a restart.
func New(cfg Config) (*slog.Logger, *slog.LevelVar) {
	h, level := NewHandler(cfg)
	return slog.New(h), level
}

func presentation(groups []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.TimeKey:
		if len(groups) == 0 {
			// UTC, so two replicas' logs interleave correctly.
			if t := a.Value.Time(); !t.IsZero() {
				a.Value = slog.StringValue(t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00"))
			}
		}
	case slog.LevelKey:
		if len(groups) == 0 {
			if l, ok := a.Value.Any().(slog.Level); ok {
				a.Value = slog.StringValue(strings.ToLower(l.String()))
			}
		}
	case slog.SourceKey:
		if src, ok := a.Value.Any().(*slog.Source); ok && src != nil {
			// A full build path is noise and leaks the builder's home
			// directory into every line.
			src.File = trimToRepo(src.File)
		}
	}
	return a
}

func trimToRepo(path string) string {
	const marker = "cofound" + string(filepath.Separator)
	if i := strings.LastIndex(path, marker); i >= 0 {
		return path[i+len(marker):]
	}
	return filepath.Base(path)
}

// Install makes l the process default and returns a *log.Logger for
// http.Server.ErrorLog.
//
// Both halves matter. slog.SetDefault means a third-party library's log.Printf
// becomes JSON through the Guard instead of unstructured stderr. The returned
// logger means net/http's own errors — TLS handshake failures, malformed
// requests — stop bypassing the pipeline entirely.
func Install(l *slog.Logger, h slog.Handler) *log.Logger {
	slog.SetDefault(l)
	return StdLogger(h, slog.LevelWarn)
}

// StdLogger adapts a slog.Handler to the standard log package.
func StdLogger(h slog.Handler, level slog.Level) *log.Logger {
	return slog.NewLogLogger(h, level)
}
