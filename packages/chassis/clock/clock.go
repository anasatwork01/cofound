// Package clock is the chassis's single injected time dependency.
//
// Fake lives in this non-test file on purpose: the readiness registry, the
// shutdown sequence and the stream heartbeat all need to be tested without
// waiting in wall-clock time, and a fake confined to one package's _test.go
// cannot be shared.
package clock

import (
	"sync"
	"time"
)

// Clock is the subset of time this module uses. Kept deliberately small — every
// method here has to be faithfully faked.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
	After(d time.Duration) <-chan time.Time
	NewTicker(d time.Duration) Ticker
}

// Ticker mirrors *time.Ticker through an interface so Fake can supply one.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type realClock struct{}

// Real returns a Clock backed by the time package.
func Real() Clock { return realClock{} }

func (realClock) Now() time.Time                  { return time.Now() }
func (realClock) Since(t time.Time) time.Duration { return time.Since(t) }

func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func (realClock) NewTicker(d time.Duration) Ticker { return realTicker{time.NewTicker(d)} }

type realTicker struct{ t *time.Ticker }

func (r realTicker) C() <-chan time.Time { return r.t.C }
func (r realTicker) Stop()               { r.t.Stop() }

// Fake is a manually advanced Clock.
//
// Waiters is the handshake that keeps tests race-free: a test that advances time
// before the code under test has actually started waiting would advance past a
// deadline nobody was watching, and then hang. Wait for the expected number of
// waiters, then Advance.
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	waiters []*fakeWaiter
}

type fakeWaiter struct {
	at     time.Time
	ch     chan time.Time
	period time.Duration // non-zero for a ticker
	closed bool
}

// NewFake returns a Fake reading now.
func NewFake(now time.Time) *Fake { return &Fake{now: now} }

// Now returns the fake's current time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Since returns the fake's elapsed time.
func (f *Fake) Since(t time.Time) time.Duration { return f.Now().Sub(t) }

// After registers a one-shot wait.
func (f *Fake) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := &fakeWaiter{at: f.now.Add(d), ch: make(chan time.Time, 1)}
	f.waiters = append(f.waiters, w)
	return w.ch
}

// NewTicker registers a repeating wait.
func (f *Fake) NewTicker(d time.Duration) Ticker {
	if d <= 0 {
		panic("clock: NewTicker requires a positive period")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	w := &fakeWaiter{at: f.now.Add(d), ch: make(chan time.Time, 1), period: d}
	f.waiters = append(f.waiters, w)
	return &fakeTicker{f: f, w: w}
}

// Advance moves the clock forward and fires every wait that is now due.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	now := f.now
	var fire []*fakeWaiter
	kept := f.waiters[:0]
	for _, w := range f.waiters {
		if w.closed {
			continue
		}
		if !w.at.After(now) {
			fire = append(fire, w)
			if w.period > 0 {
				// Re-arm from the fired deadline, not from now, so a long
				// Advance does not silently skip periods.
				for !w.at.After(now) {
					w.at = w.at.Add(w.period)
				}
				kept = append(kept, w)
			}
			continue
		}
		kept = append(kept, w)
	}
	f.waiters = kept
	f.mu.Unlock()

	for _, w := range fire {
		select {
		case w.ch <- now:
		default: // a pending unread tick is dropped, as time.Ticker does
		}
	}
}

// Set moves the clock to an absolute time, firing anything now due.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	d := t.Sub(f.now)
	f.mu.Unlock()
	if d > 0 {
		f.Advance(d)
		return
	}
	f.mu.Lock()
	f.now = t
	f.mu.Unlock()
}

// Waiters reports how many waits are currently registered.
func (f *Fake) Waiters() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, w := range f.waiters {
		if !w.closed {
			n++
		}
	}
	return n
}

type fakeTicker struct {
	f *Fake
	w *fakeWaiter
}

func (t *fakeTicker) C() <-chan time.Time { return t.w.ch }

func (t *fakeTicker) Stop() {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	t.w.closed = true
}
