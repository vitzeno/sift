package runtime

import (
	"github.com/vitzeno/sift/internal/eval"
	"github.com/vitzeno/sift/internal/value"
)

// Declassify applies Fn (mask/hash/redact) to each named column's value
// in place (design-improvements.md §4), sharing eval.Declassify with the
// expression-position function call: one implementation behind both
// namespaces (design-improvements.md §6).
type Declassify struct {
	in      Stream
	fn      string
	columns []string
}

func NewDeclassify(in Stream, fn string, columns []string) *Declassify {
	return &Declassify{in: in, fn: fn, columns: columns}
}

func (d *Declassify) Next() (value.Row, bool) {
	row, ok := d.in.Next()
	if !ok {
		return value.Row{}, false
	}
	if row.Fail != nil {
		return row, true
	}
	fields := make(map[string]any, len(row.Fields))
	for k, v := range row.Fields {
		fields[k] = v
	}
	for _, name := range d.columns {
		// A column can be both Optional and @pii (design/optional-fields.md
		// §4). An absent value has nothing to declassify, so it passes
		// through untouched instead of panicking the type assertion
		// below on a value that was never there.
		if _, absent := fields[name].(value.Absent); absent {
			continue
		}
		fields[name] = eval.Declassify(d.fn, fields[name].(string))
	}
	return value.Row{Fields: fields, Prov: row.Prov}, true
}
