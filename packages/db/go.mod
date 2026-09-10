// Package db is the control plane's Postgres access layer.
//
// A separate module from packages/chassis on purpose: the chassis deliberately
// has no database dependency, so that a service which does not talk to Postgres
// -- and the chassis's own tests -- do not build pgx. errs/constructors.go says
// so where it string-matches "no rows in result set"; ErrNoRows here is what it
// was waiting for.
module github.com/anasatwork01/cofound/packages/db

// See packages/chassis/go.mod for why this is pinned rather than aspirational:
// `go work sync` rewrites the directive to the maximum across the dependency
// graph, so committing anything lower leaves a dirty tree after bootstrap.
go 1.27.1

require (
	github.com/exaring/otelpgx v0.12.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.11.0
	github.com/pressly/goose/v3 v3.28.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/sethvargo/go-retry v0.4.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/sdk v1.46.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)

replace github.com/anasatwork01/cofound/packages/chassis => ../chassis

replace github.com/anasatwork01/cofound/packages/schema/gen/go => ../schema/gen/go
