// Package eval implements design.md §4's eval(expr, row) any: computing
// an expression's runtime value against one row, no bytecode, by
// switching on AST node types and applying Go's own operators.
//
// Every expression handled here has already passed the checker
// (internal/checker), so a type mismatch found mid-evaluation (a field
// missing from the row, an operand of the wrong Go type) is an internal
// invariant violation, not a user-facing data error, and CLAUDE.md
// reserves panic for exactly that case. A genuine per-row data problem
// (a malformed CSV cell) is already caught earlier, in the source
// (internal/format/csvSource), so by the time a row reaches eval its
// shape is guaranteed.
package eval

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
	"github.com/vitzeno/sift/internal/value"
)

// Eval computes expr's value against row. The Go type returned for each
// value.Kind matches what internal/format's csvSource already produces
// for that kind (int for Int, float64 for Double, string for String,
// bool for Bool): one consistent runtime representation used everywhere
// a Row.Fields value is read or written.
func Eval(expr ast.Expr, row value.Row) any {
	switch e := expr.(type) {
	case *ast.FieldAccess:
		return row.Fields[e.Field]

	case *ast.IntLit:
		return int(e.Value)

	case *ast.DoubleLit:
		return e.Value

	case *ast.StringLit:
		return e.Value

	case *ast.BoolLit:
		return e.Value

	case *ast.BinaryOp:
		return evalBinaryOp(e, row)

	case *ast.Call:
		return evalCall(e, row)

	case *ast.RecordExpr:
		panic("eval: record literal is not a scalar expression (only valid as map's argument)")

	default:
		panic(fmt.Sprintf("eval: unhandled expression type %T", expr))
	}
}

// EvalRecord computes a map stage's output fields from its record
// literal: a spread copies every field already on row, then each
// explicit field is evaluated and set, overriding a spread-copied value
// of the same name. This mirrors internal/checker's checkMapRecord
// exactly (spread, then override-or-append): one computes the type of
// the transformation ahead of time, the other performs it, and the rule
// itself must agree between them.
func EvalRecord(rec *ast.RecordExpr, row value.Row) map[string]any {
	fields := make(map[string]any, len(rec.Fields))
	if rec.Spread != "" {
		for k, v := range row.Fields {
			fields[k] = v
		}
	}
	for _, f := range rec.Fields {
		fields[f.Name] = Eval(f.Value, row)
	}
	return fields
}

// isAbsent reports whether v is an optional field's absent value
// (value.Absent), the runtime counterpart of value.Type.Optional the
// checker tracked ahead of time (design/optional-fields.md §3).
func isAbsent(v any) bool {
	_, ok := v.(value.Absent)
	return ok
}

func evalBinaryOp(e *ast.BinaryOp, row value.Row) any {
	// ?? is the one operator that resolves an Absent left operand, so it
	// must inspect left before the general Absent short-circuit below
	// would otherwise swallow it.
	if e.Op == lexer.COALESCE {
		left := Eval(e.Left, row)
		if isAbsent(left) {
			return Eval(e.Right, row)
		}
		return left
	}

	left := Eval(e.Left, row)
	right := Eval(e.Right, row)
	// Every other operator propagates Absent the way the checker's
	// Optional tag propagated at compile time: an absent operand yields
	// an absent result, never a type-assertion panic below on a value
	// that was never there.
	if isAbsent(left) || isAbsent(right) {
		return value.Absent{}
	}

	switch e.Op {
	case lexer.PLUS:
		switch l := left.(type) {
		case int:
			return l + right.(int)
		case float64:
			return l + right.(float64)
		case string:
			return l + right.(string)
		}
	case lexer.MINUS:
		switch l := left.(type) {
		case int:
			return l - right.(int)
		case float64:
			return l - right.(float64)
		}
	case lexer.STAR:
		switch l := left.(type) {
		case int:
			return l * right.(int)
		case float64:
			return l * right.(float64)
		}
	case lexer.SLASH:
		switch l := left.(type) {
		case int:
			return l / right.(int)
		case float64:
			return l / right.(float64)
		}
	case lexer.LT:
		switch l := left.(type) {
		case int:
			return l < right.(int)
		case float64:
			return l < right.(float64)
		case value.DateValue:
			return dateCompare(l, right) < 0
		}
	case lexer.GT:
		switch l := left.(type) {
		case int:
			return l > right.(int)
		case float64:
			return l > right.(float64)
		case value.DateValue:
			return dateCompare(l, right) > 0
		}
	case lexer.LE:
		switch l := left.(type) {
		case int:
			return l <= right.(int)
		case float64:
			return l <= right.(float64)
		case value.DateValue:
			return dateCompare(l, right) <= 0
		}
	case lexer.GE:
		switch l := left.(type) {
		case int:
			return l >= right.(int)
		case float64:
			return l >= right.(float64)
		case value.DateValue:
			return dateCompare(l, right) >= 0
		}
	case lexer.EQ:
		// decision: value.DateValue wraps time.Time, a struct whose bare
		// == compares internal representation (wall/ext fields, a
		// *Location pointer), not the instant it denotes -- two equal
		// dates aren't guaranteed == (design/date.md §3). Every other
		// Kind here is a plain comparable Go primitive, so bare == is
		// correct for them; only Date needs its own branch.
		if l, ok := left.(value.DateValue); ok {
			return time.Time(l).Equal(time.Time(right.(value.DateValue)))
		}
		return left == right
	case lexer.NE:
		if l, ok := left.(value.DateValue); ok {
			return !time.Time(l).Equal(time.Time(right.(value.DateValue)))
		}
		return left != right
	case lexer.AND:
		return left.(bool) && right.(bool)
	case lexer.OR:
		return left.(bool) || right.(bool)
	}
	panic(fmt.Sprintf("eval: unhandled binary operator %s on %T", e.Op, left))
}

// dateCompare orders two value.DateValue operands via time.Time.Compare
// (design/date.md §3), returning -1/0/1: the same instant-aware
// comparison EQ/NE's Equal() dispatch above uses, not Go's bare struct
// ordering (which value.DateValue, wrapping time.Time, doesn't even
// support directly with < > <= >=).
func dateCompare(l value.DateValue, right any) int {
	return time.Time(l).Compare(time.Time(right.(value.DateValue)))
}

// evalCall implements what the checker only typed: the actual behavior
// of v0's closed function set. Every entry takes one string and returns
// one string (internal/checker's builtinFuncs), so evalCall needs no
// arity/type dispatch of its own; the checker already guaranteed both by
// the time this runs.
func evalCall(e *ast.Call, row value.Row) any {
	argVal := Eval(e.Args[0], row)
	if isAbsent(argVal) {
		// The checker lets a function propagate Optional unconditionally
		// (design/optional-fields.md §3: upper(.phone) is string?). Mirror
		// that here instead of asserting an Absent to string.
		return value.Absent{}
	}
	arg := argVal.(string)
	switch e.Fn {
	case "mask", "hash", "redact":
		return Declassify(e.Fn, arg)
	case "upper":
		return strings.ToUpper(arg)
	case "lower":
		return strings.ToLower(arg)
	case "trim":
		return strings.TrimSpace(arg)
	default:
		panic(fmt.Sprintf("eval: unknown function %q (checker should have rejected this)", e.Fn))
	}
}

// Declassify applies a named declassifier (mask/hash/redact) to s. It's
// exported so runtime's Declassify stage (design-improvements.md §4,
// §6's "two namespaces") shares the exact same implementation as
// evalCall's expression-position call: one implementation, two call
// sites, so "mask" can't drift into two different meanings.
//
// decision: design.md names mask/hash/redact as PII declassifiers but
// never specifies their algorithms. Picked the simplest reasonable,
// deterministic behavior for each, using only the standard library
// (CLAUDE.md: "no third-party deps in the core"):
//   - mask:   same length, every character replaced with '*'. Shows the
//     value's shape without its content.
//   - hash:   SHA-256, hex-encoded. A real one-way hash, not a stub.
//   - redact: a fixed placeholder, dropping length and shape entirely
//     (the strictest of the three).
func Declassify(fn, s string) string {
	switch fn {
	case "mask":
		return strings.Repeat("*", len(s))
	case "hash":
		sum := sha256.Sum256([]byte(s))
		return fmt.Sprintf("%x", sum)
	case "redact":
		return "[REDACTED]"
	default:
		panic(fmt.Sprintf("eval: unknown declassifier %q", fn))
	}
}
