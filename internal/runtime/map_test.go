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
