// Package lifecycle owns the shutdown sequence and the long-lived-request
// drain.
//
// It exists because http.Server.Shutdown does not cancel in-flight request
// contexts. That is documented — go doc net/http.Request.Context lists client
// close, HTTP/2 cancel and ServeHTTP returning, and Shutdown is not among them
// — and measured: a naive Shutdown with one open SSE handler returns
// "context deadline exceeded" after its full deadline and then cuts the stream
// mid-write. So the broadcast has to be ours.
package lifecycle

import (
	"context"
	"maps"
	"sync"
)

// Drain tracks long-lived requests and broadcasts that shutdown has begun.
//
// It is a type rather than a bare channel so that an SSE handler's entire
// involvement is two lines, and so "did we drain cleanly, and what was still
// open" becomes a logged fact rather than a guess.
type Drain struct {
	once sync.Once
	ch   chan struct{}

	mu      sync.Mutex
	active  map[string]int
	total   int
	waiters []chan struct{}
	begun   bool
}

// NewDrain returns a Drain that has not begun.
func NewDrain() *Drain {
	return &Drain{ch: make(chan struct{}), active: map[string]int{}}
}

// Draining is closed when shutdown begins. A stream selects on it.
func (d *Drain) Draining() <-chan struct{} {
	if d == nil {
		// A nil Drain never fires, which lets a handler run outside the
		// chassis stack without a nil check.
		return nil
	}
	return d.ch
}

// IsDraining reports whether shutdown has begun.
func (d *Drain) IsDraining() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.begun
}

// Enter registers a long-lived request of the given kind.
//
// ok is false once draining has begun, so a new stream arriving mid-shutdown is
// refused with a 503 and a Retry-After rather than being started and then
// immediately cut — the client can reconnect to a healthy replica instead of
// burning a round trip on this one.
func (d *Drain) Enter(kind string) (release func(), ok bool) {
	if d == nil {
		return func() {}, true
	}
	d.mu.Lock()
	if d.begun {
		d.mu.Unlock()
		return func() {}, false
	}
	d.active[kind]++
	d.total++
	d.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			d.mu.Lock()
			d.active[kind]--
			if d.active[kind] <= 0 {
				delete(d.active, kind)
			}
			d.total--
			var wake []chan struct{}
			if d.total == 0 {
				wake, d.waiters = d.waiters, nil
			}
			d.mu.Unlock()
			for _, w := range wake {
				close(w)
			}
		})
	}, true
}

// Open returns the per-kind count of active long-lived requests.
func (d *Drain) Open() map[string]int {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return maps.Clone(d.active)
}

// Begin broadcasts that shutdown has started. Idempotent.
func (d *Drain) Begin() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.begun = true
	d.mu.Unlock()
	d.once.Do(func() { close(d.ch) })
}

// Wait blocks until no long-lived request is active, or ctx expires.
func (d *Drain) Wait(ctx context.Context) error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	if d.total == 0 {
		d.mu.Unlock()
		return nil
	}
	w := make(chan struct{})
	d.waiters = append(d.waiters, w)
	d.mu.Unlock()

	select {
	case <-w:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
