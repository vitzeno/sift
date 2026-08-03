// Package eval implements design.md §4's eval(expr, row) any: computing
// an expression's runtime value against one row, no bytecode, by
// switching on AST node types and applying Go's own operators.
//
// Every expression handled here has already passed the checker
// (internal/checker), so a type mismatch encountered mid-evaluation
// (a field missing from the row, an operand of the wrong Go type) is an
// internal invariant violation, not a user-facing data error — CLAUDE.md
// reserves panic for exactly that case. A genuine per-row data problem
// (a malformed CSV cell) is already caught earlier, in the source
// (internal/format/csvSource); by the time a row reaches eval, its shape
// is guaranteed.
package eval

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
	"github.com/vitzeno/sift/internal/value"
)

// Eval computes expr's value against row. The Go type returned for each
// value.Kind matches what internal/format's csvSource already produces
// for that kind (int for Int, float64 for Double, string for String,
// bool for Bool) — one consistent runtime representation used
// everywhere a Row.Fields value is read or written.
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
// exactly (spread, then override-or-append) — one computes the type of
// the transformation ahead of time, the other performs it, but the rule
// itself is defined once in each and must agree.
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

func evalBinaryOp(e *ast.BinaryOp, row value.Row) any {
	left := Eval(e.Left, row)
	right := Eval(e.Right, row)

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
		}
	case lexer.GT:
		switch l := left.(type) {
		case int:
			return l > right.(int)
		case float64:
			return l > right.(float64)
		}
	case lexer.LE:
		switch l := left.(type) {
		case int:
			return l <= right.(int)
		case float64:
			return l <= right.(float64)
		}
	case lexer.GE:
		switch l := left.(type) {
		case int:
			return l >= right.(int)
		case float64:
			return l >= right.(float64)
		}
	case lexer.EQ:
		return left == right
	case lexer.NE:
		return left != right
	case lexer.AND:
		return left.(bool) && right.(bool)
	case lexer.OR:
		return left.(bool) || right.(bool)
	}
	panic(fmt.Sprintf("eval: unhandled binary operator %s on %T", e.Op, left))
}

// evalCall implements what the checker only typed: the actual behavior
// of v0's closed function set. Every entry takes one string and returns
// one string (internal/checker's builtinFuncs), so evalCall doesn't need
// its own arity/type dispatch — the checker already guaranteed both by
// the time this runs.
func evalCall(e *ast.Call, row value.Row) any {
	arg := Eval(e.Args[0], row).(string)
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
// evalCall's expression-position call — one implementation, two call
// sites, never two definitions of what "mask" means to drift apart.
//
// decision: design.md names mask/hash/redact as PII declassifiers but
// never specifies their algorithms. Picked the simplest reasonable,
// deterministic behavior for each, using only the standard library
// (CLAUDE.md: "no third-party deps in the core"):
//   - mask:   same length, every character replaced with '*' — shows the
//     value's shape without its content.
//   - hash:   SHA-256, hex-encoded — a real one-way hash, not a stub.
//   - redact: a fixed placeholder, dropping length/shape entirely (the
//     strictest of the three).
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
