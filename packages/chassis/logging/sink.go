package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
)

// Sink collects emitted log lines for assertions.
//
// It lives in a non-test file deliberately: every package's tests then assert
// against real JSON produced by the real production handler chain, rather than
// against a stub that might not share the behaviour under test. That is what
// makes the redaction tests proof instead of approximation.
type Sink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// NewSink returns an empty Sink.
func NewSink() *Sink { return &Sink{} }

// Write implements io.Writer.
func (s *Sink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// Lines returns each emitted line decoded from JSON. A line that does not parse
// is skipped, which keeps a text-format sink usable for Contains.
func (s *Sink) Lines() []map[string]any {
	s.mu.Lock()
	raw := s.buf.String()
	s.mu.Unlock()

	var out []map[string]any
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err == nil {
			out = append(out, m)
		}
	}
	return out
}

// Last returns the most recent decoded line, or nil.
func (s *Sink) Last() map[string]any {
	lines := s.Lines()
	if len(lines) == 0 {
		return nil
	}
	return lines[len(lines)-1]
}

// Contains reports whether the raw output contains substr. This is the
// assertion that matters for a leak test: it looks at the bytes actually
// written, not at a parsed view that might normalise something away.
func (s *Sink) Contains(substr string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Contains(s.buf.String(), substr)
}

// String returns the raw output.
func (s *Sink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// Reset discards everything collected so far.
func (s *Sink) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf.Reset()
}
