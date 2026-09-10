// Checked in and hand-maintained — NOT generated, unlike every .gen.go
// file beside it. scripts/gen.sh preserves this file deliberately: letting
// `go mod tidy` re-resolve these versions on every run would make the
// schema drift check fail whenever a dependency published a release.
//
// If generated code gains a new import, `go build` names the missing
// module; add the require here and commit go.sum with it.
module github.com/anasatwork01/cofound/packages/schema/gen/go

go 1.27.1

require github.com/oapi-codegen/runtime v1.7.0

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/stretchr/testify v1.12.1 // indirect
)
