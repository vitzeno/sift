package runtime

import (
	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/eval"
	"github.com/vitzeno/sift/internal/value"
)

// Filter keeps rows from in for which pred returns true, per design.md
// §7's trace: a false predicate re-pulls from in rather than returning,
// so a run of rejected rows costs no output but still visits every input
// row exactly once.
type Filter struct {
	in   Stream
	pred func(value.Row) bool
}

// NewFilter builds a Filter from a plain Go predicate. Used directly by
// tests that want to exercise the re-pull loop in isolation, without
// pulling in the AST/eval machinery NewFilterExpr needs.
func NewFilter(in Stream, pred func(value.Row) bool) *Filter {
	return &Filter{in: in, pred: pred}
}

// NewFilterExpr builds a Filter from a checked ast.Expr — module 2's
// decision to defer this until eval existed (module 7). The re-pull loop
// in Next below is untouched; only how a predicate is evaluated changed.
func NewFilterExpr(in Stream, pred ast.Expr) *Filter {
	return NewFilter(in, func(row value.Row) bool {
		return eval.Eval(pred, row).(bool)
	})
}

func (f *Filter) Next() (value.Row, bool) {
	for {
		row, ok := f.in.Next()
		if !ok {
			return value.Row{}, false
		}
		// A failed row is opaque: it flows straight through, never
		// tested against pred (design-errors.md §2.2). Its Fields may
		// be incomplete or suspect, and it's the driver's job — not
		// Filter's — to decide what happens to it.
		if row.Fail != nil {
			return row, true
		}
		if f.pred(row) {
			return row, true
		}
	}
}
