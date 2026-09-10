package httpx

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anasatwork01/cofound/packages/chassis/clock"
	"github.com/anasatwork01/cofound/packages/chassis/errs"
	"github.com/anasatwork01/cofound/packages/chassis/lifecycle"
)

// HeaderLastEventID is the SSE resume header (SPEC 3.1).
const HeaderLastEventID = "Last-Event-ID"

// Event is one SSE frame.
type Event struct {
	ID    string
	Name  string
	Data  []byte
	Retry time.Duration
}

// Encode renders the frame. Pure, so the wire format is unit-testable.
func (e Event) Encode() []byte {
	var b bytes.Buffer
	if e.ID != "" {
		b.WriteString("id: ")
		b.WriteString(sanitizeField(e.ID))
		b.WriteString("\n")
	}
	if e.Name != "" {
		b.WriteString("event: ")
		b.WriteString(sanitizeField(e.Name))
		b.WriteString("\n")
	}
	if e.Retry > 0 {
		b.WriteString("retry: ")
		b.WriteString(strconv.FormatInt(e.Retry.Milliseconds(), 10))
		b.WriteString("\n")
	}
	// Multi-line data must be folded into one data: line per line, or the
	// client reassembles it wrongly. CRLF and CR are normalised first.
	data := strings.ReplaceAll(string(e.Data), "\r\n", "\n")
	data = strings.ReplaceAll(data, "\r", "\n")
	if data == "" {
		b.WriteString("data: \n")
	} else {
		for _, line := range strings.Split(data, "\n") {
			b.WriteString("data: ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	return b.Bytes()
}

// sanitizeField strips newlines from a single-line field, which would otherwise
// let a caller inject extra frames.
func sanitizeField(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(s)
}

// CloseReason records why a stream ended.
type CloseReason string

const (
	ClosedDone       CloseReason = "done"
	ClosedDraining   CloseReason = "draining"
	ClosedClientGone CloseReason = "client_gone"
)

// StreamOptions configures a stream.
type StreamOptions struct {
	// Kind names the stream for drain accounting, e.g. "session_events".
	Kind      string
	Heartbeat time.Duration
	Clock     clock.Clock
}

// Stream is an open SSE response.
type Stream struct {
	w       http.ResponseWriter
	rc      *http.ResponseController
	ctx     context.Context
	cancel  context.CancelFunc
	release func()
	drain   *lifecycle.Drain
	lastID  string
	kind    string

	deadlineCleared bool
	closed          bool
}

// Open starts an SSE response.
//
// It is the only sanctioned way to open one, because it does three things a
// handler must not be able to forget: clears the per-request write deadline,
// sets the stream headers, and takes a drain slot. That is what makes shutdown
// physically unable to hang on a stream.
func Open(w http.ResponseWriter, r *http.Request, o StreamOptions) (*Stream, error) {
	kind := o.Kind
	if kind == "" {
		kind = "stream"
	}
	drain := DrainFrom(r.Context())
	release, ok := drain.Enter(kind)
	if !ok {
		// Draining already began. Starting the stream now would cut it
		// immediately; a 503 lets the client reconnect to a healthy replica.
		return nil, errs.Unavailable("this server", nil).
			WithMessage("This server is shutting down.").
			WithFix("Reconnect; another replica will serve the stream.").
			WithRetryAfter(time.Second)
	}

	rc := http.NewResponseController(w)

	// FIRST act, before any header or byte. WriteTimeout applies to the whole
	// response, so a live stream is cut at the deadline: measured as 3 of 8
	// events then "unexpected EOF" at a 500ms WriteTimeout, and 8 of 8 with the
	// deadline cleared. ReadTimeout, ReadHeaderTimeout and IdleTimeout do NOT
	// cut a live stream — only WriteTimeout does.
	deadlineCleared := rc.SetWriteDeadline(time.Time{}) == nil

	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("X-Accel-Buffering", "no")
	if r.ProtoMajor == 1 {
		h.Set("Connection", "close")
	}
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()

	// One context cancelled by EITHER a client disconnect or the drain
	// broadcast. Folding them together is what reduces a handler's entire
	// shutdown participation to using s.Context() instead of r.Context().
	ctx, cancel := context.WithCancel(r.Context())
	s := &Stream{
		w: w, rc: rc, ctx: ctx, cancel: cancel, release: release, drain: drain,
		lastID: r.Header.Get(HeaderLastEventID), kind: kind,
		deadlineCleared: deadlineCleared,
	}
	if ch := drain.Draining(); ch != nil {
		go func() {
			select {
			case <-ch:
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	return s, nil
}

// Send writes one event and flushes.
func (s *Stream) Send(e Event) error {
	if _, err := s.w.Write(e.Encode()); err != nil {
		return errs.From(err)
	}
	if err := s.rc.Flush(); err != nil {
		return errs.From(err)
	}
	return nil
}

// Comment writes a keepalive comment, which holds an idle connection open
// through proxies without producing a client-visible event.
func (s *Stream) Comment(text string) error {
	if _, err := s.w.Write([]byte(": " + sanitizeField(text) + "\n\n")); err != nil {
		return errs.From(err)
	}
	return s.rc.Flush()
}

// Context is cancelled on client disconnect OR drain.
//
// Using s.Context() instead of r.Context() is the WHOLE of a handler's
// shutdown participation.
func (s *Stream) Context() context.Context { return s.ctx }

// Draining reports the shutdown broadcast directly, for a handler that wants to
// distinguish it from a disconnect.
func (s *Stream) Draining() <-chan struct{} { return s.drain.Draining() }

// IsDraining reports whether shutdown has begun.
func (s *Stream) IsDraining() bool { return s.drain.IsDraining() }

// LastEventID returns the client's resume point (SPEC 3.1).
func (s *Stream) LastEventID() string { return s.lastID }

// DeadlineCleared reports whether the write deadline was actually cleared.
//
// It returns false when a middleware in the chain wrapped the ResponseWriter
// without Unwrap, which silently breaks streaming. Assertable, so a test can
// catch that regression.
func (s *Stream) DeadlineCleared() bool { return s.deadlineCleared }

// Fail writes an agent-events error frame, for a failure after the stream
// opened and a status code is no longer available.
func (s *Stream) Fail(err error, turn int) error {
	e := errs.From(err)
	body, mErr := marshalEvent(e.Event(turn))
	if mErr != nil {
		return mErr
	}
	return s.Send(Event{Name: "error", Data: body})
}

// Close ends the stream, announcing why.
func (s *Stream) Close(reason CloseReason) error {
	if s.closed {
		return nil
	}
	s.closed = true
	defer func() {
		s.cancel()
		s.release()
	}()

	if reason == ClosedDraining {
		// Tell the client this was deliberate and where to resume, so it
		// reconnects immediately instead of treating it as an error.
		_ = s.Send(Event{Name: "shutdown", ID: s.lastID, Data: []byte(`{"reason":"draining"}`)})
	}
	return nil
}

// Kind returns the stream's drain kind.
func (s *Stream) Kind() string { return s.kind }

func marshalEvent(v any) ([]byte, error) {
	body, err := jsonMarshal(v)
	if err != nil {
		return nil, errs.Internal(fmt.Errorf("marshal stream event: %w", err))
	}
	return body, nil
}
