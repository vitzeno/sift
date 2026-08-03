package runtime

import (
	"testing"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
	"github.com/vitzeno/sift/internal/value"
)

func TestCheckPassesRowsMeetingCondition(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"email": "ada@example.com"}},
		{Fields: map[string]any{"email": "tom@example.com"}},
	}}
	c := NewCheck(src, &ast.BinaryOp{
		Op:    lexer.NE,
		Left:  &ast.FieldAccess{Field: "email"},
		Right: &ast.StringLit{Value: ""},
	}, "missing email")
	sink := &fakeSink{}

	if err := Run(c, src, []Sink{sink}, PolicyAbort, nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(sink.written) != 2 {
		t.Errorf("sink received %d rows, want 2", len(sink.written))
	}
}

// TestCheckSetsFailOnFalseCondition confirms design-errors.md §2.3: a
// false condition marks the row with Fail (Stage "check") and returns it
// normally — Check itself never aborts the run; that's the driver's job.
func TestCheckSetsFailOnFalseCondition(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"email": ""}, Prov: value.Provenance{Source: "in", Ordinal: 0}},
	}}
	c := NewCheck(src, &ast.BinaryOp{
		Op:    lexer.NE,
		Left:  &ast.FieldAccess{Field: "email"},
		Right: &ast.StringLit{Value: ""},
	}, "missing email")

	row, ok := c.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want the failed row returned normally")
	}
	if row.Fail == nil {
		t.Fatal("Fail is nil, want it set")
	}
	if row.Fail.Reason != "missing email" {
		t.Errorf("Fail.Reason = %q, want %q", row.Fail.Reason, "missing email")
	}
	if row.Fail.Stage != "check" {
		t.Errorf("Fail.Stage = %q, want %q", row.Fail.Stage, "check")
	}
}

// TestCheckPassesThroughAlreadyFailedRow confirms Check doesn't
// re-evaluate its condition against a row that already failed upstream
// — design-errors.md §2.2's opaque-failed-row rule applies to check
// itself, not just to Filter/Map.
func TestCheckPassesThroughAlreadyFailedRow(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fail: &value.Failure{Reason: "earlier failure", Stage: "csv:age"}},
	}}
	c := NewCheck(src, &ast.BinaryOp{
		Op:    lexer.NE,
		Left:  &ast.FieldAccess{Field: "email"}, // would panic: "email" isn't in Fields
		Right: &ast.StringLit{Value: ""},
	}, "missing email")

	row, ok := c.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want the failed row passed through")
	}
	if row.Fail.Reason != "earlier failure" {
		t.Errorf("Fail.Reason = %q, want the original failure preserved", row.Fail.Reason)
	}
}

// TestCheckFailureAbortsRunAndClosesSink covers design.md §2's default
// error policy: a failed check stops the whole run. Run must surface a
// *FailureError and still close the sink so its file handle isn't
// leaked, even though the run didn't complete successfully.
func TestCheckFailureAbortsRunAndClosesSink(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"email": "ada@example.com"}, Prov: value.Provenance{Source: "in", Ordinal: 0}},
		{Fields: map[string]any{"email": ""}, Prov: value.Provenance{Source: "in", Ordinal: 1}},
		{Fields: map[string]any{"email": "tom@example.com"}, Prov: value.Provenance{Source: "in", Ordinal: 2}},
	}}
	c := NewCheck(src, &ast.BinaryOp{
		Op:    lexer.NE,
		Left:  &ast.FieldAccess{Field: "email"},
		Right: &ast.StringLit{Value: ""},
	}, "missing email")
	sink := &fakeSink{}

	err := Run(c, src, []Sink{sink}, PolicyAbort, nil)
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
	// Only Ada's row, before the failure, should have reached the sink.
	if len(sink.written) != 1 {
		t.Errorf("sink received %d rows, want 1", len(sink.written))
	}
	if !sink.closed {
		t.Error("sink.Close was not called after the abort")
	}
}

// TestCheckFailureSkippedUnderPolicySkip is ERR-B at Check's level:
// under PolicySkip the same failing program drops the bad row, keeps
// going, and completes with no error.
func TestCheckFailureSkippedUnderPolicySkip(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"email": "ada@example.com"}, Prov: value.Provenance{Source: "in", Ordinal: 0}},
		{Fields: map[string]any{"email": ""}, Prov: value.Provenance{Source: "in", Ordinal: 1}},
		{Fields: map[string]any{"email": "tom@example.com"}, Prov: value.Provenance{Source: "in", Ordinal: 2}},
	}}
	c := NewCheck(src, &ast.BinaryOp{
		Op:    lexer.NE,
		Left:  &ast.FieldAccess{Field: "email"},
		Right: &ast.StringLit{Value: ""},
	}, "missing email")
	sink := &fakeSink{}

	if err := Run(c, src, []Sink{sink}, PolicySkip, nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(sink.written) != 2 {
		t.Fatalf("sink received %d rows, want 2 (Ada and Tom, the failure dropped)", len(sink.written))
	}
	if sink.written[0].Fields["email"] != "ada@example.com" || sink.written[1].Fields["email"] != "tom@example.com" {
		t.Errorf("sink.written = %+v, want Ada then Tom", sink.written)
	}
	if !sink.closed {
		t.Error("sink.Close was not called")
	}
}
