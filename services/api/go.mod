module github.com/anasatwork01/cofound/services/api

// 1.27.1, not 1.25.0. `go work sync` — which `make bootstrap` runs — rewrites a
// module's go directive to the maximum across its dependency graph, and the
// chassis is at 1.27.1, so committing anything lower leaves a dirty tree on the
// next bootstrap. gitd, aigw, mcp and agentd have no such requires and stay at
// 1.25.0; verified that go work sync leaves them alone.
go 1.27.1

require (
	github.com/anasatwork01/cofound/packages/chassis v0.0.0
	github.com/anasatwork01/cofound/packages/schema/gen/go v0.0.0
)

require (
	github.com/anasatwork01/cofound/packages/db v0.0.0
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/go-chi/chi/v5 v5.3.2 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.30.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.71.0 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/sdk v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260819154853-08b0e4226688 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260819154853-08b0e4226688 // indirect
	google.golang.org/grpc v1.83.1 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

// Both replaces are mandatory, not stylistic. The repository is public, so
// without them these paths resolve against proxy.golang.org and fail on "no
// matching versions" — and they fail LATE, naming a source file rather than the
// missing directive, so it reads as a network problem.
replace github.com/anasatwork01/cofound/packages/chassis => ../../packages/chassis

replace github.com/anasatwork01/cofound/packages/schema/gen/go => ../../packages/schema/gen/go

replace github.com/anasatwork01/cofound/packages/db => ../../packages/db
