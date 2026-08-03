package checker

import (
	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/value"
)

// checkSelect computes select's output schema: exactly the named
// columns, in the order named (design-improvements.md §1). Each name
// must exist in the input schema; a duplicate name is rejected.
func (c *checker) checkSelect(st *ast.Select, schema value.Schema) (value.Schema, error) {
	seen := map[string]bool{}
	fields := make([]value.Field, 0, len(st.Columns))
	for _, col := range st.Columns {
		if seen[col.Name] {
			return value.Schema{}, errorf(col.Pos, "duplicate column %q in select", col.Name)
		}
		seen[col.Name] = true

		f, ok := schema.Lookup(col.Name)
		if !ok {
			return value.Schema{}, errorf(col.Pos, "column %q not in schema %s", col.Name, schema)
		}
		fields = append(fields, f)
	}
	return value.Schema{Fields: fields}, nil
}

// checkDrop computes drop's output schema: the input schema minus the
// named columns, with every surviving column keeping its original order
// (design-improvements.md §1). Each name must exist in the input schema;
// dropping every column (an empty result schema) is rejected.
func (c *checker) checkDrop(st *ast.Drop, schema value.Schema) (value.Schema, error) {
	toDrop := map[string]bool{}
	for _, col := range st.Columns {
		if _, ok := schema.Lookup(col.Name); !ok {
			return value.Schema{}, errorf(col.Pos, "column %q not in schema %s", col.Name, schema)
		}
		toDrop[col.Name] = true
	}

	var fields []value.Field
	for _, f := range schema.Fields {
		if !toDrop[f.Name] {
			fields = append(fields, f)
		}
	}
	if len(fields) == 0 {
		return value.Schema{}, errorf(st.Pos, "drop leaves no columns; schema was %s", schema)
	}
	return value.Schema{Fields: fields}, nil
}
