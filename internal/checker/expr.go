package checker

import (
	"fmt"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
	"github.com/vitzeno/sift/internal/value"
)

// funcSig is a built-in scalar function's signature. The function table
// is closed and every entry takes exactly one string and returns a
// string, so one shape covers all of it. Declassify marks the three that
// clear @pii.
type funcSig struct {
	Param      value.Kind
	Return     value.Kind
	Declassify bool
}

// builtinFuncs is the complete function table. User-defined scalar
// functions don't exist yet; this table can grow a lookup for them later
// without changing checkCall's shape.
var builtinFuncs = map[string]funcSig{
	"mask":   {Param: value.String, Return: value.String, Declassify: true},
	"hash":   {Param: value.String, Return: value.String, Declassify: true},
	"redact": {Param: value.String, Return: value.String, Declassify: true},
	"upper":  {Param: value.String, Return: value.String},
	"lower":  {Param: value.String, Return: value.String},
	"trim":   {Param: value.String, Return: value.String},
}

// checkExpr computes an expression's value.Type against schema,
// propagating @pii as it goes: any expression that consumes a @pii value
// yields a @pii result, uniformly across every operator and function
// (except the three declassifiers).
func (c *checker) checkExpr(e ast.Expr, schema value.Schema) (value.Type, error) {
	switch e := e.(type) {
	case *ast.FieldAccess:
		f, ok := schema.Lookup(e.Field)
		if !ok {
			return value.Type{}, errorf(e.Pos, "field %q not in schema %s", e.Field, schema)
		}
		// Every way a @deidentify column could be consumed -- a filter
		// predicate, a comparison, a function argument, a segment
		// parameter's substituted body -- reads the column first, so
		// blocking that read here is the one place that covers all of
		// them: nothing downstream ever sees a Deidentified value to
		// reject a second time.
		if f.Type.Kind == value.Deidentified {
			return value.Type{}, errorf(e.Pos, "field %q is @deidentify and cannot be used in an expression; it can only be passed through, selected, dropped, or renamed", e.Field)
		}
		return f.Type, nil

	case *ast.ParamRef:
		// A bare identifier that survived to ordinary checking was never
		// substituted for a scalar parameter's argument: either it's
		// outside any parameterized segment body, or
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
		// A record literal has no scalar type (value.Kind has no record
		// variant). It's only meaningful as map's direct
		// argument, handled by checkMapRecord, never as a general
		// sub-expression.
		return value.Type{}, errorf(e.Pos, "record literal is only valid as map's argument")

	default:
		panic(fmt.Sprintf("checker: unhandled expression type %T", e))
	}
}

// checkBinaryOp types the binary operators.
//
// There is no implicit int<->double promotion: + - * / require both
// operands to already share the same Kind. A coercion matrix would be
// unused surface area; a future `1 + 1.5` can be supported by loosening
// this one function later.
func (c *checker) checkBinaryOp(e *ast.BinaryOp, schema value.Schema) (value.Type, error) {
	left, err := c.checkExpr(e.Left, schema)
	if err != nil {
		return value.Type{}, err
	}
	right, err := c.checkExpr(e.Right, schema)
	if err != nil {
		return value.Type{}, err
	}

	// A bare int/double literal standing directly against a decimal
	// operand adapts to decimal in that position only, mirroring Go's own
	// untyped-constant model.
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
	// Optional propagates exactly like PII: any operator consuming a T?
	// yields a U? until ?? explicitly
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

// checkCoalesce types the `??` discharge operator: left ?? right always
// yields a non-Optional result. right supplies the value for left's
// absent case, so after ?? there is no absent case left to track.
//
// right (the default) must share left's Kind and must not itself be
// Optional. Requiring that keeps "?? always discharges" a hard
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

// isNumeric includes Decimal: + - * / and the comparison operators all
// accept it on equal footing with int/double.
func isNumeric(k value.Kind) bool {
	return k == value.Int || k == value.Double || k == value.Decimal
}

// literalKind reports e's Kind and true if e is a bare numeric literal
// node (ast.IntLit or ast.DoubleLit) -- never a field access or a
// computed expression, even one that happens to be int/double-typed.
// The only caller is checkBinaryOp's decimal literal-context exception:
// an int/double column must never silently adapt to decimal, only a
// literal constant written at the call site.
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

// isOrderable reports whether < > <= >= accept k: every numeric Kind,
// plus Date and DateTime, both comparable but deliberately not numeric
// (no +, -, *, / on either — isNumeric stays unchanged). Date and
// DateTime never mix with each other here: this only says k itself is
// orderable, and checkBinaryOp's separate left.Kind != right.Kind
// rejection is what actually keeps the two temporal Kinds from comparing
// against one another.
func isOrderable(k value.Kind) bool {
	return isNumeric(k) || k == value.Date || k == value.DateTime
}

func isNumericOrString(k value.Kind) bool {
	return isNumeric(k) || k == value.String
}

// checkCall types a function call against builtinFuncs. An unrecognized
// name is a compile error, not a silent no-op: the function set is
// closed, same as the stage set.
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
	// unconditionally: upper(.phone) on a string? is itself a string?.
	return value.Type{Kind: sig.Return, Optional: argType.Optional, PII: pii}, nil
}
