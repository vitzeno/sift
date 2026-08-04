package runtime

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
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

	if err := Run(filtered, src, []Sink{sink}, nil, PolicyAbort, nil); err != nil {
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

	if err := Run(src, src, []Sink{sink}, nil, PolicyAbort, nil); err != nil {
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

	err := Run(src, src, []Sink{sink}, nil, PolicyAbort, nil)
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

	if err := Run(src, src, []Sink{sink}, nil, PolicySkip, nil); err != nil {
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
// Err() aborts the run even under PolicySkip. Infra-fatal isn't
// governed by the error policy at all (design-errors.md §2.4).
func TestDriverInfraFatalAbortsRegardlessOfPolicy(t *testing.T) {
	infraErr := fmt.Errorf("disk read error")
	src := &fakeStream{err: infraErr}
	sink := &fakeSink{}

	err := Run(src, src, []Sink{sink}, nil, PolicySkip, nil)
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

// TestDriverBroadcastsToEveryMainSink is design-multisink.md's terminal
// broadcast at the runtime layer: every healthy row reaches every sink,
// in declared order, and Close is called on all of them.
func TestDriverBroadcastsToEveryMainSink(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"name": "Ada", "age": 42}},
	}}
	sink1 := &fakeSink{}
	sink2 := &fakeSink{}

	if err := Run(src, src, []Sink{sink1, sink2}, nil, PolicyAbort, nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(sink1.written) != 1 || len(sink2.written) != 1 {
		t.Fatalf("sink1/sink2 received %d/%d rows, want 1/1", len(sink1.written), len(sink2.written))
	}
	if !sink1.closed || !sink2.closed {
		t.Error("every sink must be closed, even when there's more than one")
	}
}

// failingCloseSink always errors on Close, so tests can force the
// close-all path to hit more than one failure.
type failingCloseSink struct {
	fakeSink
	closeErr error
}

func (s *failingCloseSink) Close() error {
	s.fakeSink.Close()
	return s.closeErr
}

// TestDriverCloseAllAggregatesErrors is design-multisink.md §6's
// "close-all" rule: every sink is closed even if an earlier one errors,
// and the failures are aggregated rather than the first one winning.
func TestDriverCloseAllAggregatesErrors(t *testing.T) {
	src := &fakeStream{}
	sink1 := &failingCloseSink{closeErr: fmt.Errorf("disk full")}
	sink2 := &failingCloseSink{closeErr: fmt.Errorf("permission denied")}

	err := Run(src, src, []Sink{sink1, sink2}, nil, PolicyAbort, nil)
	if err == nil {
		t.Fatal("Run succeeded, want the aggregated close errors")
	}
	if !sink1.closed || !sink2.closed {
		t.Error("both sinks must be closed even though both error")
	}
	if !strings.Contains(err.Error(), "disk full") || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error = %q, want it to contain both close errors", err.Error())
	}
}

// TestDriverRouteWritesEnvelopeToErrSink is ERR-C at the runtime layer:
// under PolicyRoute, a healthy row still reaches the main sink, and a
// failed row's envelope (design-errors.md §4), not its raw fields,
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

	if err := Run(src, src, []Sink{sink}, nil, PolicyRoute, errSink); err != nil {
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

// TestDriverRouteFirstMatchWins is RT-A at the runtime layer: a row
// satisfying two branches' predicates lands in the earlier one's sink
// only, and evaluation stops there. The later branch (and its sink)
// never sees the row at all.
func TestDriverRouteFirstMatchWins(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"name": "Ada"}},
	}}
	first, second := &fakeSink{}, &fakeSink{}
	route := []RouteBranch{
		{Pred: &ast.BoolLit{Value: true}, SinkIndex: 0},
		{Pred: &ast.BoolLit{Value: true}, SinkIndex: 1},
		{IsElse: true, SinkIndex: 1},
	}

	if err := Run(src, src, []Sink{first, second}, route, PolicyAbort, nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(first.written) != 1 {
		t.Errorf("first sink received %d rows, want 1", len(first.written))
	}
	if len(second.written) != 0 {
		t.Errorf("second sink received %d rows, want 0 (first branch already matched)", len(second.written))
	}
}

// TestDriverRouteDiscardDropsRow is RT-C at the runtime layer: `else =>
// discard` drops an unmatched row with no write to any sink, and the run
// still completes with no error.
func TestDriverRouteDiscardDropsRow(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"name": "Ada"}},
	}}
	sink := &fakeSink{}
	route := []RouteBranch{
		{Pred: &ast.BoolLit{Value: false}, SinkIndex: 0},
		{IsElse: true, Discard: true, SinkIndex: -1},
	}

	if err := Run(src, src, []Sink{sink}, route, PolicyAbort, nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(sink.written) != 0 {
		t.Errorf("sink received %d rows, want 0 (discarded)", len(sink.written))
	}
	if !sink.closed {
		t.Error("sink.Close was not called")
	}
}

// TestDriverRouteNeverEvaluatesBranchOnFailedRow is RT-E: a failed row is
// disposed of by the error policy before route ever sees it, proven by
// giving every branch a predicate that panics if eval ever reaches it.
func TestDriverRouteNeverEvaluatesBranchOnFailedRow(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fail: &value.Failure{Reason: "missing email", Stage: "check"}},
	}}
	sink := &fakeSink{}
	poison := &ast.Call{Fn: "not-a-real-function", Args: []ast.Expr{&ast.StringLit{Value: "x"}}}
	route := []RouteBranch{
		{Pred: poison, SinkIndex: 0},
		{IsElse: true, SinkIndex: 0},
	}

	if err := Run(src, src, []Sink{sink}, route, PolicySkip, nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(sink.written) != 0 {
		t.Errorf("sink received %d rows, want 0 (the only row failed, and skip drops it before any branch)", len(sink.written))
	}
}

// TestDriverRouteDuplicateSinkWritesUnion is RT-F at the runtime layer:
// two branches naming the same sink both write to it, across different
// rows: the union of everything either branch matched.
func TestDriverRouteDuplicateSinkWritesUnion(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"name": "Ada", "region": "EU"}},
		{Fields: map[string]any{"name": "Tom", "region": "US"}},
	}}
	sink := &fakeSink{}
	regionEquals := func(want string) ast.Expr {
		return &ast.BinaryOp{Op: lexer.EQ, Left: &ast.FieldAccess{Field: "region"}, Right: &ast.StringLit{Value: want}}
	}
	route := []RouteBranch{
		{Pred: regionEquals("EU"), SinkIndex: 0},
		{Pred: regionEquals("US"), SinkIndex: 0},
		{IsElse: true, Discard: true, SinkIndex: -1},
	}

	if err := Run(src, src, []Sink{sink}, route, PolicyAbort, nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(sink.written) != 2 {
		t.Fatalf("sink received %d rows, want 2 (both EU and US routed to the same sink)", len(sink.written))
	}
	if sink.written[0].Fields["name"] != "Ada" || sink.written[1].Fields["name"] != "Tom" {
		t.Errorf("sink.written = %+v, want Ada then Tom", sink.written)
	}
}
