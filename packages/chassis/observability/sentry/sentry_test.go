package sentry

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	sentrygo "github.com/getsentry/sentry-go"

	"github.com/anasatwork01/cofound/packages/chassis/observability"
)

// testDSN is syntactically valid and points at a reserved TLD (RFC 2606), so a
// misbehaving test cannot reach a real ingest endpoint.
const testDSN = "https://0123456789abcdef0123456789abcdef@o0.ingest.example.invalid/42"

func testConfig() Config {
	return Config{
		DSN: testDSN, Service: "api", Version: "1.4.2", Commit: "abc1234",
		Env: "production", InstanceID: "api-7f9c",
	}
}

// newTestClient builds a client from the REAL production options, so every
// assertion below is about what a deployed service resolves rather than about
// what a test constructed.
func newTestClient(t *testing.T, opts sentrygo.ClientOptions) *sentrygo.Client {
	t.Helper()
	c, err := sentrygo.NewClient(opts)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

// TestNoDSNStartsNoGoroutines is the assertion this whole package is shaped
// around, and it exists because the failure it guards is INVISIBLE.
//
// sentry-go's Init does not become a no-op with an empty DSN. NewClient still
// builds the telemetry processor, and Scheduler.Start spawns a worker goroutine
// plus a second goroutine holding a 100ms time.Ticker — forever, in every
// service, in every environment that has no DSN: local development, CI, and any
// self-hosted deploy. Nothing logs, nothing errors, nothing behaves
// differently. Counting goroutines is the only way to see it.
//
// Not parallel: runtime.NumGoroutine is process-wide.
func TestNoDSNStartsNoGoroutines(t *testing.T) {
	if c := sentrygo.CurrentHub().Client(); c != nil {
		t.Fatal("a previous test bound a client to the global hub; this test cannot measure anything")
	}

	before := runtime.NumGoroutine()

	h, err := Init(context.Background(), Config{Service: "api", Env: "development"})
	if err != nil {
		t.Fatalf("Init with no DSN must succeed: %v", err)
	}
	if h != nil {
		t.Fatalf("Init with no DSN must return a nil *Handle, got %#v", h)
	}
	// The direct, deterministic half: sentry-go binds the client it built to
	// the current hub, so a bound client proves Init was called.
	if c := sentrygo.CurrentHub().Client(); c != nil {
		t.Fatal("sentry.Init was called with an empty DSN: a client is bound to the global hub")
	}

	// The empirical half. A goroutine started by NewClient exists the moment
	// the `go` statement runs and never exits, so the count coming back to the
	// baseline at any point proves none were started. Polling rather than
	// sampling once absorbs unrelated goroutines the test binary's own runtime
	// may be retiring, instead of flaking on them.
	deadline := time.Now().Add(2 * time.Second)
	after := before
	for {
		after = runtime.NumGoroutine()
		if after <= before {
			return
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	buf := make([]byte, 1<<16)
	buf = buf[:runtime.Stack(buf, true)]
	t.Errorf("Init with no DSN started %d goroutine(s) (%d -> %d). Stacks:\n%s",
		after-before, before, after, buf)
}

// TestResolvedDataCollectionSendsNoCookiesAndNoBodies is a SPEC 17 control, not
// a preference.
//
// sentry-go 0.48.0's changelog recommends passing &sentry.DataCollection{} to
// adopt the granular options. Doing so flips cookie collection ON under a
// denylist and turns on ALL THREE HTTP body types, because an empty struct
// resolves through resolveDataCollection rather than legacyDataCollection.
// The console's session cookie is a __Host- cookie and request bodies carry
// user content, so the one-line "upgrade" is what ships both to a third party.
// SPEC 17.3 requires no secret values and no user content.
//
// The assertions are on the RESOLVED collection rather than on the options we
// pass, so they still bite if a future sentry-go changes what nil resolves to.
func TestResolvedDataCollectionSendsNoCookiesAndNoBodies(t *testing.T) {
	opts := options(testConfig())

	// Both fields must stay at their zero values. t.Error, not t.Fatal: the
	// resolved assertions below are the ones that matter and must still run.
	if opts.DataCollection != nil {
		t.Error("ClientOptions.DataCollection must stay nil; see the comment in options()")
	}
	if opts.SendDefaultPII {
		t.Error("ClientOptions.SendDefaultPII must stay false")
	}

	dc := newTestClient(t, opts).GetDataCollection()

	if dc.Cookies == nil || dc.Cookies.Mode != sentrygo.CollectionOff {
		t.Errorf("cookies must not be collected: mode = %v, want CollectionOff (%v)",
			modeOf(dc.Cookies), sentrygo.CollectionOff)
	}
	if len(dc.HTTPBodies) != 0 {
		t.Errorf("no HTTP body type may be collected, got %v", dc.HTTPBodies)
	}
	if dc.UserInfo.Or(true) {
		t.Error("automatic user.* population must stay off")
	}
}

func modeOf(b *sentrygo.KeyValueCollectionBehavior) any {
	if b == nil {
		return "<nil>"
	}
	return b.Mode
}

// TestFactoryReturnsANilInterfaceNotATypedNil.
//
// `return Init(ctx, c)` compiles and is wrong: a nil *Handle in a
// Reporter-typed return is an interface that compares != nil, so the chassis
// would install a panic hook and a shutdown step for a reporter that does not
// exist — in exactly the no-DSN case that has to be right.
func TestFactoryReturnsANilInterfaceNotATypedNil(t *testing.T) {
	r, err := Factory(context.Background(), Config{Service: "api"})
	if err != nil {
		t.Fatalf("Factory with no DSN must succeed: %v", err)
	}
	if r != nil {
		t.Fatalf("Factory with no DSN must return a nil interface, got %#v", r)
	}

	var reporter observability.Reporter = r
	if reporter != nil {
		t.Fatal("the nil must survive assignment to the seam's interface type")
	}
}

// TestReleaseAndEnvironmentAreAlwaysExplicit.
//
// An empty Release makes NewClient walk SENTRY_RELEASE, GITHUB_SHA and eleven
// other CI variables and then shell out to `git describe`; an empty Environment
// makes it read SENTRY_ENVIRONMENT. Release regression detection is one of the
// two reasons this package exists, so what a release means must be decided by
// the chassis's own build metadata and nothing else.
func TestReleaseAndEnvironmentAreAlwaysExplicit(t *testing.T) {
	opts := options(testConfig())
	if want := "api@1.4.2+abc1234"; opts.Release != want {
		t.Errorf("Release = %q, want %q", opts.Release, want)
	}
	if opts.Environment != "production" {
		t.Errorf("Environment = %q, want production", opts.Environment)
	}

	// The case that matters more: nothing bound. Placeholders are better than
	// detection — "halyard@dev" is obviously a local build, whereas a release
	// silently taken from GITHUB_SHA looks authoritative and is not.
	bare := options(Config{DSN: testDSN})
	if bare.Release == "" {
		t.Error("an empty Release hands release detection to sentry-go's git and CI probing")
	}
	if bare.Environment == "" {
		t.Error("an empty Environment hands the environment to SENTRY_ENVIRONMENT")
	}
	if strings.Contains(bare.Release, "@@") {
		t.Errorf("Release = %q", bare.Release)
	}
}

// TestOtelIntegrationIsRegistered. It is the entire OpenTelemetry story here:
// it resolves the active trace id from the context so a captured issue and its
// trace point at each other. Without it the two observability systems in this
// repo would be unjoinable.
func TestOtelIntegrationIsRegistered(t *testing.T) {
	opts := options(testConfig())
	if opts.Integrations == nil {
		t.Fatal("no Integrations hook")
	}
	defaults := []sentrygo.Integration{stubIntegration{}}
	got := opts.Integrations(defaults)

	var sawOTel, sawDefault bool
	for _, in := range got {
		switch in.Name() {
		case "OTel":
			sawOTel = true
		case "stub":
			sawDefault = true
		}
	}
	if !sawOTel {
		t.Error("the OTel linking integration is not registered")
	}
	if !sawDefault {
		t.Error("the default integrations must be kept, not replaced")
	}
}

type stubIntegration struct{}

func (stubIntegration) Name() string               { return "stub" }
func (stubIntegration) SetupOnce(*sentrygo.Client) {}

// TestShutdownAbandonsCloseWhenTheBudgetExpires.
//
// Client.Close takes no context. It hardcodes 5s and spends it twice —
// Scheduler.Stop flushes for the timeout, then waits for the timeout again —
// and with a transport that never answers it does not return AT ALL: an
// unbounded Close was measured here holding a test binary past 120s.
// lifecycle.PhaseTelemetry's budget is single-digit seconds with the
// orchestrator's termination grace period behind it, and overrunning that means
// SIGKILL mid-write — the unclean cut that Last-Event-ID resume exists to make
// unnecessary.
//
// The wedged transport is the point: with an idle client Close returns in
// microseconds, so a test that merely times a healthy Close asserts nothing.
func TestShutdownAbandonsCloseWhenTheBudgetExpires(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })

	opts := options(testConfig())
	opts.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		<-block
		return nil, errors.New("wedged")
	})}
	c, err := sentrygo.NewClient(opts)
	if err != nil {
		t.Fatal(err)
	}
	// Something must be in flight, or there is nothing for Close to block on.
	ev := sentrygo.NewEvent()
	ev.Message = "held by the wedged transport"
	c.CaptureEvent(ev, nil, sentrygo.NewScope())
	time.Sleep(250 * time.Millisecond)

	h := &Handle{client: c}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- h.Shutdown(ctx) }()

	select {
	case err := <-done:
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("Shutdown took %s on an expired budget", elapsed)
		}
		if err == nil {
			t.Error("abandoning Close must be reported, not swallowed")
		}
		// Idempotent: the shutdown sequence must be safe to re-enter.
		if second := h.Shutdown(context.Background()); second != err {
			t.Errorf("second Shutdown returned %v, want the first result %v", second, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown never returned: Close was not abandoned, and the process would be SIGKILLed")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestNilHandleIsUsable. "No DSN" is a nil *Handle, and a nil *Handle has to be
// as safe as a real one or every call site grows a branch.
func TestNilHandleIsUsable(t *testing.T) {
	var h *Handle
	h.CapturePanic(context.Background(), "boom")
	if err := h.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown on a nil Handle: %v", err)
	}
}

// TestAMalformedDSNFailsBootWithoutEchoingIt. A DSN that is present but wrong
// must not boot a service that then reports nothing — and sentry-go's parse
// errors quote the value they rejected, which chassis.Main prints to stderr.
func TestAMalformedDSNFailsBootWithoutEchoingIt(t *testing.T) {
	const bad = "https://not-a-dsn.example.invalid" // no public key, no project id
	h, err := Init(context.Background(), Config{DSN: bad, Service: "api", Env: "production"})
	if err == nil {
		t.Fatal("a malformed DSN must fail boot")
	}
	if h != nil {
		t.Error("no Handle on failure")
	}
	if strings.Contains(err.Error(), bad) {
		t.Errorf("the boot error echoed the DSN: %s", err)
	}
	if c := sentrygo.CurrentHub().Client(); c != nil {
		t.Fatal("a rejected DSN must leave no client bound to the global hub")
	}
}
