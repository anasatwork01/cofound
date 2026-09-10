package errs_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/schema/gen/go/common"
)

// dsn is the shape of thing that must never reach a client: a real cause from
// a failing dependency, carrying a credential and internal topology.
const dsn = "postgres://halyard:s3cr3t@db.internal:5432/halyard?sslmode=require"

// TestWireNeverCarriesTheCause is the leak canary.
//
// It is the reason Message is authored rather than derived. If a future change
// ever populates the envelope from err.Error(), this fails with the credential
// visible in the diff.
func TestWireNeverCarriesTheCause(t *testing.T) {
	t.Parallel()
	cause := fmt.Errorf("dial %s: connection refused", dsn)

	for name, e := range map[string]*errs.Error{
		"internal":    errs.Internal(cause),
		"unavailable": errs.Unavailable("the database", cause),
		"timeout":     errs.Timeout("the database", cause),
		"invalidbody": errs.InvalidBody(cause),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body, err := json.Marshal(e.Response("REQ1"))
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{dsn, "s3cr3t", "db.internal", "connection refused"} {
				if strings.Contains(string(body), forbidden) {
					t.Fatalf("envelope leaked %q:\n%s", forbidden, body)
				}
			}
			// The cause must still be available to the log and the span,
			// otherwise the fault is undiagnosable.
			if !strings.Contains(e.Error(), "s3cr3t") {
				t.Fatalf("Error() must retain the cause for the log, got %q", e.Error())
			}
		})
	}
}

// TestWireDegradesRatherThanEmittingAnInvalidEnvelope covers the encode side.
// The generated type validates on decode only, so an invalid code or empty
// message would fail inside the client's decoder with no code to branch on.
func TestWireDegradesRatherThanEmittingAnInvalidEnvelope(t *testing.T) {
	t.Parallel()

	bad := &errs.Error{Code: errs.Code("Not A Code"), Status: 500, Message: "x"}
	if got := bad.Wire("r"); got.Code != string(errs.CodeInternal) {
		t.Errorf("invalid code = %q, want it degraded to internal", got.Code)
	}

	empty := &errs.Error{Code: errs.CodeNotFound, Status: 404}
	if got := empty.Wire("r"); got.Message == "" {
		t.Error("empty message must degrade to the fallback, not serialise empty")
	}

	// Round-trip through the generated decoder: the real assertion is that a
	// client can parse what we emit.
	body, err := json.Marshal(empty.Response("REQ1"))
	if err != nil {
		t.Fatal(err)
	}
	var back common.ErrorResponse
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("the generated decoder rejected our own envelope: %v\n%s", err, body)
	}
}

// TestEveryCatalogueEntryDecodes proves the whole vocabulary survives the
// generated decoder, not just the paths a test happened to exercise.
func TestEveryCatalogueEntryDecodes(t *testing.T) {
	t.Parallel()
	for _, code := range errs.Chassis().Codes() {
		e := errs.Chassis().New(code)
		body, err := json.Marshal(e.Response("REQ1"))
		if err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		var back common.ErrorResponse
		if err := json.Unmarshal(body, &back); err != nil {
			t.Errorf("%s: generated decoder rejected it: %v\n%s", code, err, body)
		}
		if back.Error.Code != string(code) {
			t.Errorf("%s: round-tripped as %q", code, back.Error.Code)
		}
	}
}

// TestCodeIsASentinel is why Code is a string type that implements error:
// branching needs no per-code sentinel variable.
func TestCodeIsASentinel(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("wrapped: %w", errs.NotFound("project", "acme"))
	if !errors.Is(err, errs.CodeNotFound) {
		t.Error("errors.Is must match the code through a wrap")
	}
	if errors.Is(err, errs.CodeForbidden) {
		t.Error("errors.Is must not match a different code")
	}
	var e *errs.Error
	if !errors.As(err, &e) || e.Status != 404 {
		t.Errorf("errors.As must yield the typed error with its status, got %+v", e)
	}
}

// TestNotFoundCannotBecomeAnExistenceOracle pins the contract that
// api.openapi.yaml makes normative: a cross-tenant read is indistinguishable
// from absence.
func TestNotFoundCannotBecomeAnExistenceOracle(t *testing.T) {
	t.Parallel()
	e := errs.NotFound("project", "acme")
	if len(e.Details) != 1 || e.Details["resource"] != "project" {
		t.Errorf("Details = %v, want exactly {resource: project}", e.Details)
	}
	if e.Retriable {
		t.Error("a 404 is an expected outcome, not a transient fault")
	}
	// A caller-supplied identifier must not be able to inject text into a
	// message that lands in a JSON body and in log lines.
	hostile := errs.NotFound("project", "acme\n{\"level\":\"error\"}")
	if strings.Contains(hostile.Message, "\n") {
		t.Errorf("unsanitised identifier reached the message: %q", hostile.Message)
	}
}

func TestSanitizeIdent(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"acme", "acme-corp", "a.b_c-1", strings.Repeat("a", 64)} {
		if _, valid := errs.SanitizeIdent(ok); !valid {
			t.Errorf("%q should be accepted", ok)
		}
	}
	for _, bad := range []string{"", "a b", "a\nb", "a/b", "a\"b", strings.Repeat("a", 65), "café"} {
		if _, valid := errs.SanitizeIdent(bad); valid {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

// TestFromMapsUnknownErrorsToInternal is the single choke point that makes an
// unexpected error safe by default.
func TestFromMapsUnknownErrorsToInternal(t *testing.T) {
	t.Parallel()
	if got := errs.From(errors.New("boom")); got.Code != errs.CodeInternal || got.Status != 500 {
		t.Errorf("plain error -> %s/%d, want internal/500", got.Code, got.Status)
	}
	typed := errs.Forbidden("publish")
	if got := errs.From(fmt.Errorf("wrap: %w", typed)); got.Code != errs.CodeForbidden {
		t.Errorf("typed error -> %s, want forbidden", got.Code)
	}
	if got := errs.From(context.Canceled); got.Code != errs.CodeClientClosed {
		t.Errorf("context.Canceled -> %s, want client_closed", got.Code)
	}
	if errs.From(nil) != nil {
		t.Error("From(nil) must be nil")
	}
}

// TestBuildersDoNotMutateAShared Error: catalogue-derived errors are handed out
// repeatedly, so a builder that mutated in place would cross-contaminate two
// concurrent requests.
func TestBuildersDoNotMutateASharedError(t *testing.T) {
	t.Parallel()
	base := errs.Chassis().New(errs.CodeConflict).WithDetail("a", 1)

	one := base.WithDetail("b", 2).WithFix("fix one")
	two := base.WithDetail("c", 3).WithFix("fix two")

	if len(base.Details) != 1 {
		t.Errorf("base mutated: %v", base.Details)
	}
	if base.Fix == "fix one" || base.Fix == "fix two" {
		t.Errorf("base Fix mutated: %q", base.Fix)
	}
	if _, leaked := one.Details["c"]; leaked {
		t.Error("details leaked between siblings")
	}
	if _, leaked := two.Details["b"]; leaked {
		t.Error("details leaked between siblings")
	}
}

// TestFixAndRequestIDDoNotAliasTheError guards the pointer fields on the
// generated type. Taking the address of a struct field would alias a shared
// error into a response.
func TestFixAndRequestIDDoNotAliasTheError(t *testing.T) {
	t.Parallel()
	e := errs.Forbidden("publish").WithFix("original")
	w1 := e.Wire("REQ1")
	w2 := e.Wire("REQ2")

	if w1.RequestId == w2.RequestId {
		t.Error("two renders share one request id pointer")
	}
	if *w1.RequestId != "REQ1" || *w2.RequestId != "REQ2" {
		t.Errorf("request ids crossed: %q / %q", *w1.RequestId, *w2.RequestId)
	}
	*w1.Fix = "mutated"
	if e.Fix != "original" {
		t.Errorf("mutating the wire Fix changed the error: %q", e.Fix)
	}
}

// TestEventConversionExists because common.Error and agentevents.ErrorEvent are
// structurally different generated types; a hand-written field copy elsewhere
// would drift.
func TestEventRendersTheAgentEventShape(t *testing.T) {
	t.Parallel()
	ev := errs.RateLimited(0).Event(12)
	if ev.Turn != 12 || ev.Code != "rate_limited" || !ev.Retriable {
		t.Errorf("event = %+v", ev)
	}
	if ev.Message == "" {
		t.Error("the generated schema requires a non-empty message")
	}
	body, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"type":"error"`) {
		t.Errorf("missing the discriminator: %s", body)
	}
}

// TestCatalogRejectsUnusableEntries: a catalogue is built once at init, so a
// bad entry must fail loudly at construction rather than at first render.
func TestCatalogRejectsUnusableEntries(t *testing.T) {
	t.Parallel()
	cases := map[string][]errs.Entry{
		"bad code":     {{Code: "Not Valid", Status: 400, Message: "m"}},
		"empty msg":    {{Code: "ok_code", Status: 400}},
		"status < 400": {{Code: "ok_code", Status: 200, Message: "m"}},
		"duplicate": {
			{Code: "dup", Status: 400, Message: "m"},
			{Code: "dup", Status: 409, Message: "m"},
		},
	}
	for name, entries := range cases {
		if _, err := errs.NewCatalog(entries...); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// TestChassisCatalogGolden makes any change to a client-facing message, status
// or retriable flag visible in review rather than shipped silently.
func TestChassisCatalogGolden(t *testing.T) {
	t.Parallel()
	got := errs.Chassis().Dump()
	path := filepath.Join("testdata", "catalog.golden")

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (regenerate with UPDATE_GOLDEN=1 go test ./errs/...)", err)
	}
	if got != string(want) {
		t.Errorf("catalogue changed.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestInternalIsNotRetriable documents a deliberate choice: retriable promises
// that repeating the identical request COULD succeed, and for an unknown fault
// we do not know. False makes the console tell the user to contact support
// instead of spinning a retry loop against something broken.
func TestInternalIsNotRetriable(t *testing.T) {
	t.Parallel()
	if errs.Internal(errors.New("x")).Retriable {
		t.Error("internal must not be retriable")
	}
	for _, c := range []errs.Code{errs.CodeRateLimited, errs.CodeUnavailable, errs.CodeTimeout, errs.CodeNotReady} {
		if !errs.Chassis().New(c).Retriable {
			t.Errorf("%s should be retriable", c)
		}
	}
}
