package checker

import (
	"fmt"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
	"github.com/vitzeno/sift/internal/value"
)

// funcSig is a built-in scalar function's signature. v0's function table
// is closed and every entry takes exactly one string and returns a
// string, so one shape covers all of it. Declassify marks the three
// that clear @pii (design.md §3, rule 4).
type funcSig struct {
	Param      value.Kind
	Return     value.Kind
	Declassify bool
}

// builtinFuncs is the complete v0 function table. Custom scalar
// functions (design.md §4) are optional for v0 and unused by either
// acceptance case (see ast.Program's decision comment); this table can
// grow a lookup for them later without changing checkCall's shape.
var builtinFuncs = map[string]funcSig{
	"mask":   {Param: value.String, Return: value.String, Declassify: true},
	"hash":   {Param: value.String, Return: value.String, Declassify: true},
	"redact": {Param: value.String, Return: value.String, Declassify: true},
	"upper":  {Param: value.String, Return: value.String},
	"lower":  {Param: value.String, Return: value.String},
	"trim":   {Param: value.String, Return: value.String},
}

// checkExpr computes an expression's value.Type against schema,
// propagating @pii per design.md §3 rule 2 as it goes: any expression
// that consumes a @pii value yields a @pii result, uniformly across
// every operator and function (except the three declassifiers).
func (c *checker) checkExpr(e ast.Expr, schema value.Schema) (value.Type, error) {
	switch e := e.(type) {
	case *ast.FieldAccess:
		f, ok := schema.Lookup(e.Field)
		if !ok {
			return value.Type{}, errorf(e.Pos, "field %q not in schema %s", e.Field, schema)
		}
		return f.Type, nil

	case *ast.ParamRef:
		// A bare identifier that survived to ordinary checking was never
		// substituted for a scalar parameter's argument (design/segments.md
		// §2.2): either it's outside any parameterized segment body, or
		// it doesn't name one of the enclosing segment's own parameters.
		// Either way it's an undefined name, the same diagnostic an
		// unresolvable stage NameRef gets.
		return value.Type{}, errorf(e.Pos, "undefined name %q", e.Name)

	case *ast.IntLit:
		return value.Type{Kind: value.Int}, nil

	case *ast.DoubleLit:
		return value.Type{Kind: value.Double}, nil

	case *ast.StringLit:
		return value.Type{Kind: value.String}, nil

	case *ast.BoolLit:
		return value.Type{Kind: value.Bool}, nil

	case *ast.BinaryOp:
		return c.checkBinaryOp(e, schema)

	case *ast.Call:
		return c.checkCall(e, schema)

	case *ast.RecordExpr:
		// A record literal has no scalar type in v0 (value.Kind has no
		// record variant). It's only meaningful as map's direct
		// argument, handled by checkMapRecord, never as a general
		// sub-expression.
		return value.Type{}, errorf(e.Pos, "record literal is only valid as map's argument")

	default:
		panic(fmt.Sprintf("checker: unhandled expression type %T", e))
	}
}

// checkBinaryOp types design.md §2's binary operators.
//
// decision: no implicit int<->double promotion. + - * / require both
// operands to already share the same Kind; design.md shows no example
// mixing them, and adding a coercion matrix now would be unused surface
// area (CLAUDE.md non-negotiable #2). A future `1 + 1.5` can be
// supported by loosening this one function later.
func (c *checker) checkBinaryOp(e *ast.BinaryOp, schema value.Schema) (value.Type, error) {
	left, err := c.checkExpr(e.Left, schema)
	if err != nil {
		return value.Type{}, err
	}
	right, err := c.checkExpr(e.Right, schema)
	if err != nil {
		return value.Type{}, err
	}

	// decision: a bare int/double literal standing directly against a
	// decimal operand adapts to decimal in that position only
	// (design/decimal.md §2) -- mirrors Go's own untyped-constant model.
	// Checked before every operator dispatch below, ?? included: a real
	// column of a different Kind never mixes this way, only a literal
	// constant does. Mutating the local left/right copies is enough --
	// nothing past this point reads the original ast.Expr nodes.
	if left.Kind == value.Decimal && right.Kind != value.Decimal {
		if _, ok := literalKind(e.Right); ok {
			right.Kind = value.Decimal
		}
	} else if right.Kind == value.Decimal && left.Kind != value.Decimal {
		if _, ok := literalKind(e.Left); ok {
			left.Kind = value.Decimal
		}
	}

	if e.Op == lexer.COALESCE {
		return c.checkCoalesce(e, left, right)
	}

	pii := left.PII || right.PII
	// Optional propagates exactly like PII (design/optional-fields.md §3):
	// any operator consuming a T? yields a U? until ?? explicitly
	// discharges it, above. Kind mismatches below are unaffected, since
	// optionality never changes what Kinds an operator accepts.
	optional := left.Optional || right.Optional

	switch e.Op {
	case lexer.PLUS:
		if left.Kind != right.Kind || !isNumericOrString(left.Kind) {
			return value.Type{}, errorf(e.Pos, "cannot apply + to %s and %s", left, right)
		}
		return value.Type{Kind: left.Kind, Optional: optional, PII: pii}, nil

	case lexer.MINUS, lexer.STAR, lexer.SLASH:
		if left.Kind != right.Kind || !isNumeric(left.Kind) {
			return value.Type{}, errorf(e.Pos, "cannot apply %s to %s and %s", e.Op.Symbol(), left, right)
		}
		return value.Type{Kind: left.Kind, Optional: optional, PII: pii}, nil

	case lexer.LT, lexer.GT, lexer.LE, lexer.GE:
		if left.Kind != right.Kind || !isOrderable(left.Kind) {
			return value.Type{}, errorf(e.Pos, "cannot compare %s and %s", left, right)
		}
		return value.Type{Kind: value.Bool, Optional: optional, PII: pii}, nil

	case lexer.EQ, lexer.NE:
		if left.Kind != right.Kind {
			return value.Type{}, errorf(e.Pos, "cannot compare %s and %s", left, right)
		}
		return value.Type{Kind: value.Bool, Optional: optional, PII: pii}, nil

	case lexer.AND, lexer.OR:
		if left.Kind != value.Bool || right.Kind != value.Bool {
			return value.Type{}, errorf(e.Pos, "%s requires bool operands, got %s and %s", e.Op.Symbol(), left, right)
		}
		return value.Type{Kind: value.Bool, Optional: optional, PII: pii}, nil

	default:
		panic(fmt.Sprintf("checker: unhandled binary operator %s", e.Op))
	}
}

// checkCoalesce types design/optional-fields.md §3's `??` discharge
// operator: left ?? right always yields a non-Optional result. right
// supplies the value for left's absent case, so after ?? there is no
// absent case left to track.
//
// decision: right (the default) must share left's Kind and must not
// itself be Optional. Every acceptance example (§8) supplies a concrete
// default (a literal or an already-resolved expression), never another
// optional field. Requiring that keeps "?? always discharges" a hard
// guarantee rather than a maybe; chained optional defaults (`.a ?? .b`
// where .b is itself optional) can be added later if a real program
// needs it.
func (c *checker) checkCoalesce(e *ast.BinaryOp, left, right value.Type) (value.Type, error) {
	if left.Kind != right.Kind {
		return value.Type{}, errorf(e.Pos, "?? requires both sides to share a type, got %s and %s", left, right)
	}
	if right.Optional {
		return value.Type{}, errorf(e.Pos, "?? default must not itself be optional, got %s", right)
	}
	return value.Type{Kind: left.Kind, PII: left.PII || right.PII}, nil
}

// isNumeric includes Decimal (design/decimal.md §3): + - * / and the
// comparison operators all accept it on equal footing with int/double.
func isNumeric(k value.Kind) bool {
	return k == value.Int || k == value.Double || k == value.Decimal
}

// literalKind reports e's Kind and true if e is a bare numeric literal
// node (ast.IntLit or ast.DoubleLit) -- never a field access or a
// computed expression, even one that happens to be int/double-typed.
// The only caller is checkBinaryOp's decimal literal-context exception
// (design/decimal.md §2); an int/double column must never silently
// adapt to decimal, only a literal constant written at the call site.
func literalKind(e ast.Expr) (value.Kind, bool) {
	switch e.(type) {
	case *ast.IntLit:
		return value.Int, true
	case *ast.DoubleLit:
		return value.Double, true
	default:
		return 0, false
	}
}

// isOrderable reports whether < > <= >= accept k (design/date.md §3,
// widened by design/datetime.md §3): every numeric Kind, plus Date and
// DateTime, both comparable but deliberately not numeric (no +, -, *, /
// on either — isNumeric stays unchanged). Date and DateTime never mix
// with each other here: this only says k itself is orderable, and
// checkBinaryOp's separate left.Kind != right.Kind rejection is what
// actually keeps the two temporal Kinds from comparing against one
// another (design/datetime.md §2).
func isOrderable(k value.Kind) bool {
	return isNumeric(k) || k == value.Date || k == value.DateTime
}

func isNumericOrString(k value.Kind) bool {
	return isNumeric(k) || k == value.String
}

// checkCall types a function call against builtinFuncs. An unrecognized
// name is a compile error, not a silent no-op: v0's function set is
// closed, same as its stage set (design.md §4).
func (c *checker) checkCall(e *ast.Call, schema value.Schema) (value.Type, error) {
	sig, ok := builtinFuncs[e.Fn]
	if !ok {
		return value.Type{}, errorf(e.Pos, "unknown function %q", e.Fn)
	}
	if len(e.Args) != 1 {
		return value.Type{}, errorf(e.Pos, "%s() takes 1 argument, got %d", e.Fn, len(e.Args))
	}

	argType, err := c.checkExpr(e.Args[0], schema)
	if err != nil {
		return value.Type{}, err
	}
	if argType.Kind != sig.Param {
		return value.Type{}, errorf(e.Pos, "%s() argument: expected %s, got %s", e.Fn, sig.Param, argType.Kind)
	}

	pii := argType.PII && !sig.Declassify
	// Unlike PII, no builtin function discharges Optional (only ??
	// does, in checkCoalesce). Every function call propagates it
	// unconditionally (design/optional-fields.md §3's upper(.phone) is
	// string? example).
	return value.Type{Kind: sig.Return, Optional: argType.Optional, PII: pii}, nil
}
