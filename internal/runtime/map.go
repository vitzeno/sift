package runtime

import (
	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/eval"
	"github.com/vitzeno/sift/internal/value"
)

// Map rebuilds each row from a record literal (design.md §2). Provenance
// carries over unchanged: a mapped row is still the same row, logically,
// just with different fields.
type Map struct {
	in  Stream
	rec *ast.RecordExpr
}

func NewMap(in Stream, rec *ast.RecordExpr) *Map {
	return &Map{in: in, rec: rec}
}

func (m *Map) Next() (value.Row, bool) {
	row, ok := m.in.Next()
	if !ok {
		return value.Row{}, false
	}
	return value.Row{
		Fields: eval.EvalRecord(m.rec, row),
		Prov:   row.Prov,
	}, true
}
