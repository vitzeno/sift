package runtime

import (
	"errors"
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

	if err := Run(c, sink); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(sink.written) != 2 {
		t.Errorf("sink received %d rows, want 2", len(sink.written))
	}
}

// TestCheckFailureAbortsRunAndClosesSink covers design.md §2's default
// error policy: a failed check stops the whole run. Run must surface a
// *CheckFailedError and still close the sink so its file handle isn't
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

	err := Run(c, sink)
	if err == nil {
		t.Fatal("Run succeeded, want a CheckFailedError")
	}
	var cf *CheckFailedError
	if !errors.As(err, &cf) {
		t.Fatalf("error type = %T, want *CheckFailedError", err)
	}
	if cf.Reason != "missing email" {
		t.Errorf("Reason = %q, want %q", cf.Reason, "missing email")
	}
	if cf.Prov.Ordinal != 1 {
		t.Errorf("Prov.Ordinal = %d, want 1 (the failing row)", cf.Prov.Ordinal)
	}
	// Only Ada's row, before the failure, should have reached the sink.
	if len(sink.written) != 1 {
		t.Errorf("sink received %d rows, want 1", len(sink.written))
	}
	if !sink.closed {
		t.Error("sink.Close was not called after the abort")
	}
}
