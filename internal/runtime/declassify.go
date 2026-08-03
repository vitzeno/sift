package runtime

import (
	"github.com/vitzeno/sift/internal/eval"
	"github.com/vitzeno/sift/internal/value"
)

// Declassify applies Fn (mask/hash/redact) to each named column's value
// in place (design-improvements.md §4), sharing eval.Declassify with the
// expression-position function call — one implementation behind both
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
		fields[name] = eval.Declassify(d.fn, fields[name].(string))
	}
	return value.Row{Fields: fields, Prov: row.Prov}, true
}
