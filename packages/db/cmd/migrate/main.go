// Command migrate applies the control plane migrations in db/migrations.
//
// A small binary rather than the goose CLI, for one measured reason:
// `github.com/pressly/goose/v3/cmd/goose` imports a driver for every database
// goose supports -- ClickHouse, MySQL, MSSQL, Vertica, YDB, SQLite -- so taking
// it as a `go tool` dependency pulls all of them into this module's graph for a
// Postgres-only control plane. `go mod tidy` was still resolving them after
// several minutes. The goose *library* pulls none of that; only the CLI does.
//
// Migrations are forward-only (SPEC section 0 rule 6), so there is no `down`
// subcommand here and no `-- +goose Down` in any migration. Local iteration
// resets the database instead: `make db-reset`.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	// Registers the "pgx" database/sql driver. goose speaks database/sql; the
	// pool in db.go is pgx-native and is what services use. A migration runner
	// is a short-lived single connection and needs none of that.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("dir", "db/migrations", "directory holding the migrations")
	command := flag.String("command", "up", "up, status, version or validate")
	timeout := flag.Duration("timeout", 5*time.Minute, "overall deadline")
	flag.Parse()

	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	// validate needs no database, so it can run in a lint job.
	if *command == "validate" {
		return validate(*dir)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		// sql.Open's error can carry the connection string, and the connection
		// string carries the password.
		return errors.New("the connection string could not be parsed")
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	switch *command {
	case "up":
		// goose takes a Postgres advisory lock for the duration, so two
		// replicas deploying at once do not both apply the same migration.
		return goose.UpContext(ctx, db, *dir)
	case "status":
		return goose.StatusContext(ctx, db, *dir)
	case "version":
		return goose.VersionContext(ctx, db, *dir)
	default:
		return fmt.Errorf("unknown command %q; want up, status, version or validate", *command)
	}
}

// validate checks every migration without touching a database, so a malformed
// file fails in a lint job rather than during a deploy.
//
// CollectMigrations does the part that needs goose: filenames well-formed,
// versions unique and parseable. The forward-only check is a plain text scan
// rather than an inspection of goose's parsed output, because goose.Migration
// does not expose its statements -- and a scan is what the rule actually is.
func validate(dir string) error {
	migrations, err := goose.CollectMigrations(dir, 0, goose.MaxVersion)
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return fmt.Errorf("no migrations found in %s", dir)
	}

	entries, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return err
	}
	for _, path := range entries {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		if !strings.Contains(text, "-- +goose Up") {
			return fmt.Errorf("%s has no '-- +goose Up' section", path)
		}
		if strings.Contains(text, "-- +goose Down") {
			return fmt.Errorf(
				"%s has a Down section, but migrations here are forward-only "+
					"(SPEC section 0 rule 6). Remove it; local iteration uses "+
					"`make db-reset`", path)
		}
	}

	fmt.Printf("%d migrations parse, none reversible\n", len(migrations))
	return nil
}
