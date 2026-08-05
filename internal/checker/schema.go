package checker

import (
	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/value"
)

// typeNames resolves a schema literal's raw TypeName text (design.md §4's
// compilation pipeline puts this resolution in the checker, not the
// parser) to a value.Kind. v0 had exactly the first four; date was added
// by design/date.md.
var typeNames = map[string]value.Kind{
	"string": value.String,
	"int":    value.Int,
	"double": value.Double,
	"bool":   value.Bool,
	"date":   value.Date,
}

// resolveSourceSchemas converts every source's ast.SchemaLit into a
// value.Schema, attaching the @pii tag exactly as declared (design.md
// §3, rule 1: "attach at the source").
func (c *checker) resolveSourceSchemas() error {
	c.sourceSchemas = map[string]value.Schema{}
	for _, s := range c.prog.Sources {
		seen := map[string]bool{}
		var fields []value.Field
		for _, f := range s.Schema.Fields {
			kind, ok := typeNames[f.TypeName]
			if !ok {
				return errorf(f.Pos, "unknown type %q (expected string, int, double, bool, or date)", f.TypeName)
			}
			if seen[f.Name] {
				return errorf(f.Pos, "duplicate field %q in schema", f.Name)
			}
			seen[f.Name] = true
			fields = append(fields, value.Field{
				Name: f.Name,
				Type: value.Type{Kind: kind, Optional: f.Optional, PII: f.PII},
			})
		}
		c.sourceSchemas[s.Name] = value.Schema{Fields: fields}
	}
	return nil
}

// checkMapRecord computes map's output schema from its record literal
// (design.md §3): with a spread, start from the input schema's fields in
// their original order and position; each explicit field either
// overrides an existing field in place or is appended as a new one. This
// ordering is why the jsonlSink module 2 built can rely on schema field
// order matching what a reader would expect: map never reorders a field
// it doesn't touch.
//
// Field values are checked against inputSchema, not the schema under
// construction: `{ ...row, name: upper(.name) }` reads .name from the
// row being mapped, never from a field this same record literal just
// defined.
func (c *checker) checkMapRecord(rec *ast.RecordExpr, inputSchema value.Schema) (value.Schema, error) {
	if rec.Spread != "" && rec.Spread != "row" {
		return value.Schema{}, errorf(rec.Pos,
			"unknown value %q to spread (only the current row, written \"...row\", can be spread)", rec.Spread)
	}

	var fields []value.Field
	if rec.Spread != "" {
		fields = append(fields, inputSchema.Fields...)
	}

	for _, rf := range rec.Fields {
		t, err := c.checkExpr(rf.Value, inputSchema)
		if err != nil {
			return value.Schema{}, err
		}
		newField := value.Field{Name: rf.Name, Type: t}
		if i := fieldIndex(fields, rf.Name); i >= 0 {
			fields[i] = newField
		} else {
			fields = append(fields, newField)
		}
	}
	return value.Schema{Fields: fields}, nil
}

func fieldIndex(fields []value.Field, name string) int {
	for i, f := range fields {
		if f.Name == name {
			return i
		}
	}
	return -1
}
