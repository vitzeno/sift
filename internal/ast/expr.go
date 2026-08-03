package ast

import "github.com/vitzeno/sift/internal/lexer"

// Expr is an expression: something that evaluates to a value within a
// row's scope. The exprNode marker method keeps the interface sealed to
// the types in this file, the same way go/ast seals its Expr — without
// it, any type would satisfy Expr by accident.
type Expr interface {
	exprNode()
}

// FieldAccess reads a field off the current row — `.age`, `.email`.
// There is no chained access in v0 (no `.a.b`, no indexing): every
// field access is relative to the one implicit row in scope, so a bare
// field name is all this node needs.
type FieldAccess struct {
	Field string
	Pos   lexer.Pos
}

func (*FieldAccess) exprNode() {}

type IntLit struct {
	Value int64
	Pos   lexer.Pos
}

func (*IntLit) exprNode() {}

type DoubleLit struct {
	Value float64
	Pos   lexer.Pos
}

func (*DoubleLit) exprNode() {}

type StringLit struct {
	Value string
	Pos   lexer.Pos
}

func (*StringLit) exprNode() {}

type BoolLit struct {
	Value bool
	Pos   lexer.Pos
}

func (*BoolLit) exprNode() {}

// BinaryOp is one of design.md §2's binary operators. Op reuses
// lexer.Kind directly (PLUS, LT, EQ, AND, ...) instead of a parallel
// operator enum — the token kind already names the operator, and
// converting it to a second enum would just be a lossless copy with no
// added meaning.
type BinaryOp struct {
	Op    lexer.Kind
	Left  Expr
	Right Expr
	Pos   lexer.Pos
}

func (*BinaryOp) exprNode() {}

// Call is a function call: a declassifier (mask/hash/redact), a built-in
// scalar function (upper/lower/trim), or a user-defined one. Fn is left
// as a bare name — like SourceDecl.Format, which function it resolves to
// is decided later (the checker's function-lookup table, design.md §4),
// not baked in here.
type Call struct {
	Fn   string
	Args []Expr
	Pos  lexer.Pos
}

func (*Call) exprNode() {}

// RecordField is one `name: expr` pair inside a RecordExpr.
type RecordField struct {
	Name  string
	Value Expr
	Pos   lexer.Pos
}

// RecordExpr is a record literal — `{ ...row, field: expr, ... }` — the
// sole argument to map. Spread holds the identifier named after `...`
// ("row" in every design.md example, the implicit current-row binding);
// it's the empty string when the literal has no spread at all.
type RecordExpr struct {
	Spread string
	Fields []RecordField
	Pos    lexer.Pos
}

func (*RecordExpr) exprNode() {}
