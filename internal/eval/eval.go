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
// (internal/format/csv's csvSource), so by the time a row reaches eval
// its shape is guaranteed.
package eval

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
	"github.com/vitzeno/sift/internal/value"
)

// Eval computes expr's value against row. The Go type returned for each
// value.Kind matches what internal/format/csv's csvSource already
// produces for that kind (int for Int, float64 for Double, string for
// String, bool for Bool): one consistent runtime representation used
// everywhere
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
		if a, ok := left.(value.Absent); ok {
			right := Eval(e.Right, row)
			// left is absent, so there's no DecimalValue to inspect the
			// way the promotion below relies on -- a.Kind is the only
			// surviving signal (design/decimal.md §2).
			if a.Kind == value.Decimal {
				right = toDecimal(right)
			}
			return right
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

	// decision: mirrors checkBinaryOp's literal-context promotion at the
	// type level (design/decimal.md §2). If either operand is already a
	// value.DecimalValue, the checker only ever let this expression
	// compile if the other side is an int/float64 literal, so it's safe
	// to convert that side to value.DecimalValue here too -- the switch
	// below then dispatches to decimal arithmetic regardless of which
	// side the real decimal column was physically on.
	if _, ok := left.(value.DecimalValue); ok {
		right = toDecimal(right)
	} else if _, ok := right.(value.DecimalValue); ok {
		left = toDecimal(left)
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
		case value.DecimalValue:
			return value.DecimalValue(decimal.Decimal(l).Add(asDecimal(right)))
		}
	case lexer.MINUS:
		switch l := left.(type) {
		case int:
			return l - right.(int)
		case float64:
			return l - right.(float64)
		case value.DecimalValue:
			return value.DecimalValue(decimal.Decimal(l).Sub(asDecimal(right)))
		}
	case lexer.STAR:
		switch l := left.(type) {
		case int:
			return l * right.(int)
		case float64:
			return l * right.(float64)
		case value.DecimalValue:
			return value.DecimalValue(decimal.Decimal(l).Mul(asDecimal(right)))
		}
	case lexer.SLASH:
		switch l := left.(type) {
		case int:
			return l / right.(int)
		case float64:
			return l / right.(float64)
		case value.DecimalValue:
			return value.DecimalValue(decimal.Decimal(l).Div(asDecimal(right)))
		}
	case lexer.LT:
		switch l := left.(type) {
		case int:
			return l < right.(int)
		case float64:
			return l < right.(float64)
		case value.OrderedValue:
			return l.CompareValue(right) < 0
		}
	case lexer.GT:
		switch l := left.(type) {
		case int:
			return l > right.(int)
		case float64:
			return l > right.(float64)
		case value.OrderedValue:
			return l.CompareValue(right) > 0
		}
	case lexer.LE:
		switch l := left.(type) {
		case int:
			return l <= right.(int)
		case float64:
			return l <= right.(float64)
		case value.OrderedValue:
			return l.CompareValue(right) <= 0
		}
	case lexer.GE:
		switch l := left.(type) {
		case int:
			return l >= right.(int)
		case float64:
			return l >= right.(float64)
		case value.OrderedValue:
			return l.CompareValue(right) >= 0
		}
	case lexer.EQ:
		// decision: value.DateValue and value.DecimalValue both wrap a
		// struct (time.Time, decimal.Decimal's own *big.Int underneath)
		// whose bare == compares internal representation, not the value
		// denoted -- two equal instants or amounts aren't guaranteed ==
		// (design/date.md §3, design/decimal.md §2). Every other Kind
		// here is a plain comparable Go primitive, so bare == is correct
		// for them; only a value.OrderedValue needs its own branch, one
		// shared path for every struct-backed Kind (design/datetime.md §2).
		if l, ok := left.(value.OrderedValue); ok {
			return l.CompareValue(right) == 0
		}
		return left == right
	case lexer.NE:
		if l, ok := left.(value.OrderedValue); ok {
			return l.CompareValue(right) != 0
		}
		return left != right
	case lexer.AND:
		return left.(bool) && right.(bool)
	case lexer.OR:
		return left.(bool) || right.(bool)
	}
	panic(fmt.Sprintf("eval: unhandled binary operator %s on %T", e.Op, left))
}

// asDecimal unwraps a value.DecimalValue operand to the underlying
// decimal.Decimal, for calling the library's own arithmetic methods (+ - * /
// have no shared OrderedValue-style interface, since only comparison needs
// one uniform contract across Kinds). The one place callers still need to
// know DecimalValue wraps decimal.Decimal at all (design/decimal.md §2).
func asDecimal(v any) decimal.Decimal {
	return decimal.Decimal(v.(value.DecimalValue))
}

// toDecimal converts an int or float64 literal's runtime value to
// value.DecimalValue (design/decimal.md §2's literal-context promotion,
// performed at eval time). v is never already anything else here: the
// checker only let this call site's expression compile if the operand
// this promotes is a bare int/double literal or already a DecimalValue.
func toDecimal(v any) any {
	switch v := v.(type) {
	case value.DecimalValue:
		return v
	case int:
		return value.DecimalValue(decimal.NewFromInt(int64(v)))
	case float64:
		return value.DecimalValue(decimal.NewFromFloat(v))
	default:
		panic(fmt.Sprintf("eval: cannot promote %T to decimal", v))
	}
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
