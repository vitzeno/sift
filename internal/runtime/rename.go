package runtime

import "github.com/vitzeno/sift/internal/value"

// Rename remaps each old key to its new name in a fresh Fields map;
// every other field, and every value, passes through untouched. The
// checker has already validated every
// Old exists and every New is collision-free, so Next just applies the
// mapping. There's no PII bookkeeping to do here either: the value
// itself is never touched, so whatever the checker decided about the
// @pii tag stays correct.
type Rename struct {
	in   Stream
	from map[string]string // old -> new
}

func NewRename(in Stream, pairs map[string]string) *Rename {
	return &Rename{in: in, from: pairs}
}

func (r *Rename) Next() (value.Row, bool) {
	row, ok := r.in.Next()
	if !ok {
		return value.Row{}, false
	}
	if row.Fail != nil {
		return row, true
	}
	fields := make(map[string]any, len(row.Fields))
	for k, v := range row.Fields {
		if newName, ok := r.from[k]; ok {
			fields[newName] = v
		} else {
			fields[k] = v
		}
	}
	return value.Row{Fields: fields, Prov: row.Prov}, true
}
