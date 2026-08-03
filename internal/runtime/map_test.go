package runtime

import (
	"testing"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
	"github.com/vitzeno/sift/internal/value"
)

func TestMapAppliesRecordAndKeepsProvenance(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{
			Fields: map[string]any{"name": "Ada", "age": 42},
			Prov:   value.Provenance{Source: "in", Ordinal: 0, Offset: 2},
		},
	}}
	m := NewMap(src, &ast.RecordExpr{
		Spread: "row",
		Fields: []ast.RecordField{
			{Name: "adult", Value: &ast.BinaryOp{
				Op:    lexer.GE,
				Left:  &ast.FieldAccess{Field: "age"},
				Right: &ast.IntLit{Value: 18},
			}},
		},
	})

	row, ok := m.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want a row")
	}
	if row.Fields["name"] != "Ada" || row.Fields["age"] != 42 || row.Fields["adult"] != true {
		t.Errorf("Fields = %#v, want name=Ada, age=42, adult=true", row.Fields)
	}
	if row.Prov != (value.Provenance{Source: "in", Ordinal: 0, Offset: 2}) {
		t.Errorf("Prov = %+v, want it carried over from the input row unchanged", row.Prov)
	}

	if _, ok := m.Next(); ok {
		t.Error("Next() returned ok=true past the end of the source")
	}
}

// TestMapPassesThroughFailedRowUnevaluated confirms design-errors.md
// §2.2's pass-through rule: map must not evaluate its record literal
// against a failed row's (potentially suspect) fields. A record
// expression that would panic on this row's actual Fields proves the
// point — if map evaluated it, the test itself would panic.
func TestMapPassesThroughFailedRowUnevaluated(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{
			Fields: map[string]any{"age": "not an int"}, // wrong Go type on purpose
			Fail:   &value.Failure{Reason: "bad age cell", Stage: "csv:age"},
		},
	}}
	m := NewMap(src, &ast.RecordExpr{
		Spread: "row",
		Fields: []ast.RecordField{
			{Name: "adult", Value: &ast.BinaryOp{
				Op:    lexer.GE,
				Left:  &ast.FieldAccess{Field: "age"},
				Right: &ast.IntLit{Value: 18},
			}},
		},
	})

	row, ok := m.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want the failed row passed through")
	}
	if row.Fail == nil || row.Fail.Reason != "bad age cell" {
		t.Errorf("Fail = %+v, want it carried through unchanged", row.Fail)
	}
	if row.Fields["adult"] != nil {
		t.Errorf("Fields[adult] = %#v, want map to have never evaluated the record", row.Fields["adult"])
	}
}
