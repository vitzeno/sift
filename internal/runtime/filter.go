package runtime

import "github.com/vitzeno/sift/internal/value"

// Filter keeps rows from in for which pred returns true, per design.md
// §7's trace: a false predicate re-pulls from in rather than returning,
// so a run of rejected rows costs no output but still visits every input
// row exactly once.
//
// decision: pred is a Go func(value.Row) bool for now, since eval(expr,
// row) doesn't exist until module 7. Once the checker/eval land, this
// becomes an AST expression evaluated per row; the re-pull loop below
// won't need to change.
type Filter struct {
	in   Stream
	pred func(value.Row) bool
}

func NewFilter(in Stream, pred func(value.Row) bool) *Filter {
	return &Filter{in: in, pred: pred}
}

func (f *Filter) Next() (value.Row, bool) {
	for {
		row, ok := f.in.Next()
		if !ok {
			return value.Row{}, false
		}
		if f.pred(row) {
			return row, true
		}
	}
}
