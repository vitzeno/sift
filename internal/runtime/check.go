package runtime

import (
	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/eval"
	"github.com/vitzeno/sift/internal/value"
)

// Check marks a row failed (design-errors.md §2.3) when Cond is false;
// schema/fields pass through unchanged otherwise. It never aborts the
// run itself — that decision belongs to the driver, which disposes of a
// failed row per the program's error policy (design-errors.md §3).
type Check struct {
	in     Stream
	cond   ast.Expr
	reason string
}

func NewCheck(in Stream, cond ast.Expr, reason string) *Check {
	return &Check{in: in, cond: cond, reason: reason}
}

func (c *Check) Next() (value.Row, bool) {
	row, ok := c.in.Next()
	if !ok {
		return value.Row{}, false
	}
	// Already-failed rows are opaque (design-errors.md §2.2): don't
	// re-evaluate cond against fields that are, by definition, suspect.
	if row.Fail != nil {
		return row, true
	}
	if !eval.Eval(c.cond, row).(bool) {
		row.Fail = &value.Failure{Reason: c.reason, Stage: "check"}
	}
	return row, true
}
