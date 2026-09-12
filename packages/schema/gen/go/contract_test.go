// Tests that the generated Go bindings honour the contracts the schemas state
// but the generator does not enforce.
//
// This file lives at the MODULE ROOT rather than beside the package it tests
// because scripts/gen.sh deletes every directory under gen/go before each run,
// so a test inside gen/go/common would not survive a regeneration. Root-level
// files are left alone, which is also where go.mod and go.sum live for the same
// reason.
package gengo_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/anasatwork01/cofound/packages/schema/gen/go/common"
)

// A defined type over time.Time inherits none of its methods, so without the
// generated marshaller this encodes as `{}` — time.Time's fields are all
// unexported. That is silent in Go and lands in the console as Date.parse(NaN),
// which sorts a project list alphabetically while claiming to sort by date.
func TestTimestampRoundTripsAsRFC3339(t *testing.T) {
	t.Parallel()
	// Deliberately NOT UTC: pgx returns whatever the session's location is, and
	// common.schema.json says these are always UTC.
	kolkata := time.FixedZone("IST", 5*3600+1800)
	in := common.Timestamp(time.Date(2026, 9, 12, 14, 30, 0, 0, kolkata))

	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `"2026-09-12T09:00:00Z"`; got != want {
		t.Fatalf("Marshal = %s, want %s", got, want)
	}

	var out common.Timestamp
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !time.Time(out).Equal(time.Time(in)) {
		t.Errorf("round trip = %v, want %v", time.Time(out), time.Time(in))
	}
}

// The struct case is the one that actually shipped broken: a Timestamp nested
// in a response body.
func TestTimestampMarshalsInsideAStruct(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(struct {
		CreatedAt common.Timestamp `json:"created_at"`
	}{CreatedAt: common.Timestamp(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `{"created_at":"2026-01-02T03:04:05Z"}`; got != want {
		t.Fatalf("Marshal = %s, want %s", got, want)
	}
}
