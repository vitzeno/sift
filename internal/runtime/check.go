package runtime

import (
	"fmt"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/eval"
	"github.com/vitzeno/sift/internal/value"
)

// CheckFailedError is what Run returns when a check stage's condition is
// false for some row. v0 has no configurable error policy (see
// ast.Program's decision comment) — abort is design.md §2's default and
// the only behavior v0 implements, so any failed check stops the run.
//
// decision: Stream.Next() (design.md §4) returns (Row, bool), with no
// error channel to signal this through directly. Check.Next() panics
// with this type instead, and Run recovers exactly this type and turns
// it into a returned error — the same technique the parser (module 5)
// uses for the same reason. It never escapes as an actual panic to
// anything outside this package; a failed check is an ordinary,
// expected outcome of bad input data, not an internal bug, so it must
// surface as a value (CLAUDE.md: "no bare panic on user-facing error
// paths").
type CheckFailedError struct {
	Reason string
	Prov   value.Provenance
}

func (e *CheckFailedError) Error() string {
	return fmt.Sprintf("check failed at %s (row %d): %s", e.Prov.Source, e.Prov.Ordinal, e.Reason)
}

// Check fails the run (via CheckFailedError) when Cond is false for a
// row; schema/fields pass through unchanged otherwise.
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
	if !eval.Eval(c.cond, row).(bool) {
		panic(&CheckFailedError{Reason: c.reason, Prov: row.Prov})
	}
	return row, true
}
