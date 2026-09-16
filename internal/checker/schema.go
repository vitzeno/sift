package checker

import (
	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/value"
)

// typeNames resolves a schema literal's raw TypeName text to a
// value.Kind. The parser leaves type names as text; this resolution
// belongs to the checker.
var typeNames = map[string]value.Kind{
	"string":   value.String,
	"int":      value.Int,
	"double":   value.Double,
	"bool":     value.Bool,
	"date":     value.Date,
	"decimal":  value.Decimal,
	"datetime": value.DateTime,
}

// resolveSourceSchemas converts every source's ast.SchemaLit into a
// value.Schema, attaching the @pii tag exactly as declared -- the tag
// attaches at the source. A @deidentify field resolves to
// value.Deidentified instead: the wrapping type, not another tag
// alongside Kind, with Optional discharged here
// and folded into Inner -- the field never reaches a stage as an
// optional, only Coerce needs to know it once was one.
func (c *checker) resolveSourceSchemas() error {
	c.sourceSchemas = map[string]value.Schema{}
	for _, s := range c.prog.Sources {
		seen := map[string]bool{}
		var fields []value.Field
		for _, f := range s.Schema.Fields {
			kind, ok := typeNames[f.TypeName]
			if !ok {
				return errorf(f.Pos, "unknown type %q (expected string, int, double, bool, date, decimal, or datetime)", f.TypeName)
			}
			if seen[f.Name] {
				return errorf(f.Pos, "duplicate field %q in schema", f.Name)
			}
			seen[f.Name] = true
			if f.PII && f.Deidentify {
				return errorf(f.Pos, "field %q cannot be both @pii and @deidentify; @pii tracks plaintext to a declassifier, @deidentify encrypts at the source and permits no operations", f.Name)
			}
			var typ value.Type
			if f.Deidentify {
				typ = value.Type{Kind: value.Deidentified, Inner: &value.Type{Kind: kind, Optional: f.Optional}}
			} else {
				typ = value.Type{Kind: kind, Optional: f.Optional, PII: f.PII}
			}
			fields = append(fields, value.Field{Name: f.Name, Type: typ})
		}
		c.sourceSchemas[s.Name] = value.Schema{Fields: fields}
	}
	return nil
}

// checkMapRecord computes map's output schema from its record literal:
// with a spread, start from the input schema's fields in their original
// order and position; each explicit field either overrides an existing
// field in place or is appended as a new one. This ordering is why a
// sink can rely on schema field order matching what a reader would
// expect: map never reorders a field
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
		// A @deidentify column may only be passed through (the ...row
		// spread above), never reassigned -- even to a value that never
		// reads the column back: overwriting it with a plaintext
		// constant would leave a column encrypted for
		// some rows and not others). Checked before checkExpr so the
		// diagnostic names the real problem instead of whatever the RHS
		// happens to be.
		if existing, ok := inputSchema.Lookup(rf.Name); ok && existing.Type.Kind == value.Deidentified {
			return value.Schema{}, errorf(rf.Pos, "cannot assign to %q: field is @deidentify and may not be modified; a @deidentify column can only be passed through, selected, dropped, or renamed", rf.Name)
		}

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
