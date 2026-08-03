package runtime

import (
	"testing"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
	"github.com/vitzeno/sift/internal/value"
)

// TestNewFilterExprUsesEval reproduces design.md §7's Case A trace again,
// but through NewFilterExpr (an ast.Expr evaluated by eval) instead of
// module 2's hand-wired Go predicate — fulfilling the decision comment
// left on Filter until eval existed.
func TestNewFilterExprUsesEval(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"name": "Ada", "age": 42}},
		{Fields: map[string]any{"name": "Tom", "age": 15}},
	}}
	filtered := NewFilterExpr(src, &ast.BinaryOp{
		Op:    lexer.GE,
		Left:  &ast.FieldAccess{Field: "age"},
		Right: &ast.IntLit{Value: 18},
	})
	sink := &fakeSink{}

	if err := Run(filtered, sink); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(sink.written) != 1 || sink.written[0].Fields["name"] != "Ada" {
		t.Errorf("sink.written = %+v, want exactly Ada's row", sink.written)
	}
}
