package runtime

import (
	"testing"

	"github.com/vitzeno/sift/internal/value"
)

// fakeStream replays a fixed slice of rows and counts how many times
// Next was called, so tests can assert on pull counts directly.
type fakeStream struct {
	rows  []value.Row
	calls int
}

func (f *fakeStream) Next() (value.Row, bool) {
	f.calls++
	if len(f.rows) == 0 {
		return value.Row{}, false
	}
	row := f.rows[0]
	f.rows = f.rows[1:]
	return row, true
}

// fakeSink records every Write and whether Close was called.
type fakeSink struct {
	written []value.Row
	closed  bool
}

func (s *fakeSink) Write(row value.Row) error {
	s.written = append(s.written, row)
	return nil
}

func (s *fakeSink) Close() error {
	s.closed = true
	return nil
}

// TestDriverFilterAndEOF reproduces design.md §7's trace exactly: Ada
// (age 42) passes the filter and is written; Tom (age 15) fails the
// filter, which must re-pull rather than emit or stop; the third pull
// hits EOF, and the driver breaks cleanly with no error and no extra
// write.
func TestDriverFilterAndEOF(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"name": "Ada", "age": 42}},
		{Fields: map[string]any{"name": "Tom", "age": 15}},
	}}
	filtered := NewFilter(src, func(r value.Row) bool {
		return r.Fields["age"].(int) >= 18
	})
	sink := &fakeSink{}

	if err := Run(filtered, sink); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	// Ada, Tom, then the EOF pull: exactly 3 calls to the source.
	if src.calls != 3 {
		t.Errorf("source.Next called %d times, want 3", src.calls)
	}
	// Only Ada makes it through the filter.
	if len(sink.written) != 1 {
		t.Fatalf("sink received %d rows, want 1", len(sink.written))
	}
	if got := sink.written[0].Fields["name"]; got != "Ada" {
		t.Errorf("sink wrote name=%v, want Ada", got)
	}
	if !sink.closed {
		t.Error("sink.Close was not called")
	}
}

// TestDriverEmptyStream confirms the loop breaks cleanly (no panic, no
// write) when the very first pull is EOF.
func TestDriverEmptyStream(t *testing.T) {
	src := &fakeStream{}
	sink := &fakeSink{}

	if err := Run(src, sink); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(sink.written) != 0 {
		t.Errorf("sink received %d rows, want 0", len(sink.written))
	}
	if !sink.closed {
		t.Error("sink.Close was not called")
	}
}
