# Generated bindings — do not edit

Every file here is produced by `scripts/gen.sh` from the schemas in
`packages/schema`. Editing one is pointless: the next `make gen` overwrites it,
and `make gen-check` fails the build in the meantime.

Change the schema, run `make gen`, and commit both together.

These are committed rather than ignored so that CI can prove the tree matches
the schemas, and so that building a service does not require five code
generators to be installed.

**Two exceptions:** `go/go.mod` and `go/go.sum` are checked in and
hand-maintained. `scripts/gen.sh` preserves them, because re-resolving those
versions on every run would make the drift check fail whenever a dependency
published a release. If generated code gains a new import, `go build` names the
missing module.
