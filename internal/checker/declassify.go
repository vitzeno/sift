package checker

import (
	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/value"
)

// checkDeclassify validates a declassifier stage (mask/hash/redact):
// every named column must already be string @pii (design-improvements.md
// §4). Applying a declassifier anywhere else is almost always a
// mistake and is rejected outright, the default the doc locks for v0.
// A named column's tag is cleared in place; every other column passes
// through with its type unchanged.
func (c *checker) checkDeclassify(st *ast.Declassify, schema value.Schema) (value.Schema, error) {
	fields := make([]value.Field, len(schema.Fields))
	copy(fields, schema.Fields)

	seen := map[string]bool{}
	for _, col := range st.Columns {
		if seen[col.Name] {
			return value.Schema{}, errorf(col.Pos, "duplicate column %q in %s", col.Name, st.Fn)
		}
		seen[col.Name] = true

		idx := -1
		for i, f := range fields {
			if f.Name == col.Name {
				idx = i
				break
			}
		}
		if idx == -1 {
			return value.Schema{}, errorf(col.Pos, "column %q not in schema %s", col.Name, schema)
		}

		f := fields[idx]
		if f.Type.Kind != value.String || !f.Type.PII {
			return value.Schema{}, errorf(col.Pos, "%s on non-PII column %q", st.Fn, col.Name)
		}
		fields[idx] = value.Field{Name: f.Name, Type: value.Type{Kind: value.String, PII: false}}
	}
	return value.Schema{Fields: fields}, nil
}
