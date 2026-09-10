package errs

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Entry is one row of an error catalogue.
type Entry struct {
	Code      Code
	Status    int
	Retriable bool
	Message   string
	Fix       string
}

// Catalog is an immutable set of entries.
//
// Immutable, and constructed rather than mutated at init, deliberately. A
// package-level registry written by init() is process-global mutable state: it
// makes two service test binaries in one `go test ./...` run order-dependent,
// and makes "what codes exist" untestable in isolation. Constructing one costs
// a single var per service and removes the whole class of problem.
type Catalog struct{ entries map[Code]Entry }

// NewCatalog validates and builds a catalogue.
func NewCatalog(entries ...Entry) (*Catalog, error) {
	m := make(map[Code]Entry, len(entries))
	for _, e := range entries {
		switch {
		case !e.Code.Valid():
			return nil, fmt.Errorf("errs: code %q must match ^[a-z][a-z0-9_]*$, which is what the generated decoder enforces", e.Code)
		case e.Message == "":
			// The generated schema sets minLength 1 on message and validates
			// on decode only. An empty message serialises happily here and
			// then fails inside the client's decoder, at the exact moment
			// something is already broken.
			return nil, fmt.Errorf("errs: code %q needs a message", e.Code)
		case e.Status < 400 || e.Status > 599:
			return nil, fmt.Errorf("errs: code %q has status %d, want 400..599", e.Code, e.Status)
		}
		if _, dup := m[e.Code]; dup {
			return nil, fmt.Errorf("errs: duplicate code %q", e.Code)
		}
		m[e.Code] = e
	}
	return &Catalog{entries: m}, nil
}

// MustCatalog is NewCatalog for package-level initialisation.
func MustCatalog(entries ...Entry) *Catalog {
	c, err := NewCatalog(entries...)
	if err != nil {
		panic(err)
	}
	return c
}

// chassisEntries is the transport vocabulary.
//
// internal is retriable=false deliberately. The field promises that repeating
// the identical request could succeed, and for an unknown fault we do not know
// — false makes the console say "contact support with this request id" instead
// of spinning a retry loop against a broken dependency.
var chassisEntries = []Entry{
	{CodeInvalid, 422, false, "That request is not valid.", "Check the fields and try again."},
	{CodeUnauthenticated, 401, false, "You are not signed in.", "Sign in and try again."},
	{CodePaymentRequired, 402, false, "This organisation is out of credits.", "Top up credits to continue."},
	{CodeForbidden, 403, false, "Your role does not allow this.", "Ask an owner or admin to do it, or to change your role."},
	{CodeNotFound, 404, false, "That does not exist.", ""},
	{CodeMethodNotAllowed, 405, false, "That method is not allowed here.", ""},
	{CodeConflict, 409, false, "That conflicts with the current state.", "Reload and try again."},
	{CodePayloadTooLarge, 413, false, "That request body is too large.", "Send less data."},
	{CodeUnsupportedMediaType, 415, false, "That content type is not supported.", "Send application/json."},
	{CodeRateLimited, 429, true, "Too many requests.", "Wait a moment and try again."},
	{CodeNotReady, 503, true, "This service is still starting up.", "Try again in a few seconds."},
	{CodeInternal, 500, false, "Halyard could not complete this request.", "Contact support with the request id."},
	{CodeUnavailable, 503, true, "A service Halyard depends on is unavailable.", "Try again shortly."},
	{CodeTimeout, 504, true, "That took too long.", "Try again."},
}

// ChassisEntries returns a copy of the transport vocabulary, for a service that
// extends it.
func ChassisEntries() []Entry { return slices.Clone(chassisEntries) }

var chassisCatalog = MustCatalog(chassisEntries...)

// Chassis returns the transport catalogue.
func Chassis() *Catalog { return chassisCatalog }

// Lookup returns the entry for code.
func (c *Catalog) Lookup(code Code) (Entry, bool) {
	e, ok := c.entries[code]
	return e, ok
}

// New builds an error from the catalogue, prefilled.
func (c *Catalog) New(code Code) *Error {
	e, ok := c.entries[code]
	if !ok {
		// An unknown code must not become a silently mislabelled 500 with a
		// code no client can branch on.
		return Internal(fmt.Errorf("errs: code %q is not in the catalogue", code))
	}
	return &Error{
		Code:      e.Code,
		Status:    e.Status,
		Message:   e.Message,
		Fix:       e.Fix,
		Retriable: e.Retriable,
	}
}

// Codes returns every code, sorted.
func (c *Catalog) Codes() []Code {
	out := make([]Code, 0, len(c.entries))
	for k := range maps.Keys(c.entries) {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Dump renders the catalogue for a golden file, so a change to any client-facing
// message or status is visible in review rather than shipped silently.
func (c *Catalog) Dump() string {
	var b strings.Builder
	b.WriteString("code\tstatus\tretriable\tmessage\tfix\n")
	for _, code := range c.Codes() {
		e := c.entries[code]
		b.WriteString(string(e.Code))
		b.WriteString("\t")
		b.WriteString(strconv.Itoa(e.Status))
		b.WriteString("\t")
		b.WriteString(strconv.FormatBool(e.Retriable))
		b.WriteString("\t")
		b.WriteString(e.Message)
		b.WriteString("\t")
		b.WriteString(e.Fix)
		b.WriteString("\n")
	}
	return b.String()
}
