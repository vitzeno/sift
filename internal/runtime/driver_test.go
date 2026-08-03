package runtime

import (
	"fmt"
	"testing"

	"github.com/vitzeno/sift/internal/value"
)

// fakeStream replays a fixed slice of rows and counts how many times
// Next was called, so tests can assert on pull counts directly. It also
// satisfies Source (Schema/Err), so it can stand in as Run's src
// parameter directly whenever a test has no stages wrapping it, or
// simulate an infra-fatal failure via err.
type fakeStream struct {
	rows  []value.Row
	calls int
	err   error
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

func (f *fakeStream) Schema() value.Schema {
	return value.Schema{}
}

func (f *fakeStream) Err() error {
	return f.err
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

	if err := Run(filtered, src, []Sink{sink}, PolicyAbort, nil); err != nil {
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

	if err := Run(src, src, []Sink{sink}, PolicyAbort, nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(sink.written) != 0 {
		t.Errorf("sink received %d rows, want 0", len(sink.written))
	}
	if !sink.closed {
		t.Error("sink.Close was not called")
	}
}

// TestDriverAbortOnFailedRow is ERR-A from design-errors.md §7 at the
// runtime layer: a failed row under PolicyAbort stops the run with a
// *FailureError carrying the row's reason and provenance, and closes
// the sink even though the run didn't finish. Healthy rows before the
// failure still reach the sink.
func TestDriverAbortOnFailedRow(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"name": "Ada"}, Prov: value.Provenance{Source: "in", Ordinal: 0}},
		{Fail: &value.Failure{Reason: "missing email", Stage: "check"}, Prov: value.Provenance{Source: "in", Ordinal: 1}},
		{Fields: map[string]any{"name": "Tom"}, Prov: value.Provenance{Source: "in", Ordinal: 2}},
	}}
	sink := &fakeSink{}

	err := Run(src, src, []Sink{sink}, PolicyAbort, nil)
	if err == nil {
		t.Fatal("Run succeeded, want a FailureError")
	}
	fe, ok := err.(*FailureError)
	if !ok {
		t.Fatalf("error type = %T, want *FailureError", err)
	}
	if fe.Fail.Reason != "missing email" {
		t.Errorf("Reason = %q, want %q", fe.Fail.Reason, "missing email")
	}
	if fe.Prov.Ordinal != 1 {
		t.Errorf("Prov.Ordinal = %d, want 1 (the failing row)", fe.Prov.Ordinal)
	}
	if len(sink.written) != 1 {
		t.Errorf("sink received %d rows, want 1 (only Ada, before the failure)", len(sink.written))
	}
	if !sink.closed {
		t.Error("sink.Close was not called after the abort")
	}
}

// TestDriverSkipOnFailedRow is ERR-B: the same failed row under
// PolicySkip is silently dropped, the run completes with no error, and
// every healthy row still reaches the sink.
func TestDriverSkipOnFailedRow(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"name": "Ada"}},
		{Fail: &value.Failure{Reason: "missing email", Stage: "check"}},
		{Fields: map[string]any{"name": "Tom"}},
	}}
	sink := &fakeSink{}

	if err := Run(src, src, []Sink{sink}, PolicySkip, nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(sink.written) != 2 {
		t.Fatalf("sink received %d rows, want 2 (Ada and Tom, failure dropped)", len(sink.written))
	}
	if sink.written[0].Fields["name"] != "Ada" || sink.written[1].Fields["name"] != "Tom" {
		t.Errorf("sink.written = %+v, want Ada then Tom", sink.written)
	}
	if !sink.closed {
		t.Error("sink.Close was not called")
	}
}

// TestDriverInfraFatalAbortsRegardlessOfPolicy is ERR-E: a source-level
// Err() aborts the run even under PolicySkip — infra-fatal is not
// governed by the error policy at all (design-errors.md §2.4).
func TestDriverInfraFatalAbortsRegardlessOfPolicy(t *testing.T) {
	infraErr := fmt.Errorf("disk read error")
	src := &fakeStream{err: infraErr}
	sink := &fakeSink{}

	err := Run(src, src, []Sink{sink}, PolicySkip, nil)
	if err == nil {
		t.Fatal("Run succeeded, want the infra-fatal error")
	}
	if err != infraErr {
		t.Errorf("error = %v, want the exact infra-fatal error from Err()", err)
	}
	if !sink.closed {
		t.Error("sink.Close was not called after the infra-fatal abort")
	}
}

// TestDriverRouteWritesEnvelopeToErrSink is ERR-C at the runtime layer:
// under PolicyRoute, a healthy row still reaches the main sink and a
// failed row's envelope (design-errors.md §4) — not its raw fields —
// reaches errSink instead. The run completes with no error either way.
func TestDriverRouteWritesEnvelopeToErrSink(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"name": "Ada"}, Prov: value.Provenance{Source: "in", Ordinal: 0}},
		{
			Fail: &value.Failure{Reason: "missing email", Stage: "check"},
			Prov: value.Provenance{Source: "in", Ordinal: 1, Offset: 3},
		},
		{Fields: map[string]any{"name": "Tom"}, Prov: value.Provenance{Source: "in", Ordinal: 2}},
	}}
	sink := &fakeSink{}
	errSink := &fakeSink{}

	if err := Run(src, src, []Sink{sink}, PolicyRoute, errSink); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(sink.written) != 2 {
		t.Fatalf("main sink received %d rows, want 2 (Ada and Tom)", len(sink.written))
	}
	if sink.written[0].Fields["name"] != "Ada" || sink.written[1].Fields["name"] != "Tom" {
		t.Errorf("main sink.written = %+v, want Ada then Tom", sink.written)
	}

	if len(errSink.written) != 1 {
		t.Fatalf("error sink received %d rows, want 1", len(errSink.written))
	}
	envelope := errSink.written[0]
	if envelope.Fields["source"] != "in" || envelope.Fields["ordinal"] != 1 ||
		envelope.Fields["offset"] != 3 || envelope.Fields["reason"] != "missing email" ||
		envelope.Fields["stage"] != "check" {
		t.Errorf("envelope Fields = %#v, want the full provenance+reason envelope", envelope.Fields)
	}
	// The envelope must never carry the failed row's own (possibly
	// suspect, possibly @pii) fields.
	if _, ok := envelope.Fields["name"]; ok {
		t.Error("envelope carries a raw pipeline field (\"name\") — it must only ever carry the fixed envelope fields")
	}

	if !sink.closed {
		t.Error("main sink.Close was not called")
	}
	if !errSink.closed {
		t.Error("error sink.Close was not called")
	}
}
