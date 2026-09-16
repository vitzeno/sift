package runtime

import "github.com/vitzeno/sift/internal/value"

// Select keeps only the named columns, in the order named. The checker
// has already validated every name exists in the input schema, so Next
// never needs to guard against
// a missing key: a lookup miss just means the column happens to be
// absent on this row's Fields map, which can't happen for a healthy row
// since the checker's schema and the row's actual fields always agree.
type Select struct {
	in      Stream
	columns []string
}

func NewSelect(in Stream, columns []string) *Select {
	return &Select{in: in, columns: columns}
}

func (s *Select) Next() (value.Row, bool) {
	row, ok := s.in.Next()
	if !ok {
		return value.Row{}, false
	}
	// A failed row is opaque: pass it through rather than projecting its
	// (possibly suspect) fields.
	if row.Fail != nil {
		return row, true
	}
	fields := make(map[string]any, len(s.columns))
	for _, name := range s.columns {
		fields[name] = row.Fields[name]
	}
	return value.Row{Fields: fields, Prov: row.Prov}, true
}

// Drop removes the named columns, keeping every other column.
type Drop struct {
	in      Stream
	columns map[string]bool
}

func NewDrop(in Stream, columns []string) *Drop {
	set := make(map[string]bool, len(columns))
	for _, c := range columns {
		set[c] = true
	}
	return &Drop{in: in, columns: set}
}

func (d *Drop) Next() (value.Row, bool) {
	row, ok := d.in.Next()
	if !ok {
		return value.Row{}, false
	}
	if row.Fail != nil {
		return row, true
	}
	fields := make(map[string]any, len(row.Fields))
	for k, v := range row.Fields {
		if !d.columns[k] {
			fields[k] = v
		}
	}
	return value.Row{Fields: fields, Prov: row.Prov}, true
}
