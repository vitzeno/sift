package parser

import (
	"strconv"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
)

// Precedence levels for design.md §2's binary operators, low to high.
// All are left-associative and none share design.md's grammar with a
// unary form, so there's no unary tier here at all.
//
// decision: no unary operators in v0 (no "-x", no "!x") -- design.md §2
// lists only binary + - * /, comparisons, and && / ||. A negative
// literal or a boolean negation would need one, but neither acceptance
// case calls for it, so it's left out rather than guessed at.
const (
	lowest  = 0
	orPrec  = 1
	andPrec = 2
	eqPrec  = 3
	cmpPrec = 4
	addPrec = 5
	mulPrec = 6
)

func precedenceOf(k lexer.Kind) int {
	switch k {
	case lexer.OR:
		return orPrec
	case lexer.AND:
		return andPrec
	case lexer.EQ, lexer.NE:
		return eqPrec
	case lexer.LT, lexer.GT, lexer.LE, lexer.GE:
		return cmpPrec
	case lexer.PLUS, lexer.MINUS:
		return addPrec
	case lexer.STAR, lexer.SLASH:
		return mulPrec
	default:
		return lowest
	}
}

// parseExpr parses a full expression via precedence climbing.
func (p *Parser) parseExpr() ast.Expr {
	return p.parseBinaryExpr(lowest)
}

// parseBinaryExpr implements precedence climbing: it keeps folding in
// operators strictly tighter than minPrec, and hands back to its caller
// as soon as it sees one that isn't. Recursing with the *current*
// operator's own precedence (rather than one level looser) is what makes
// same-precedence chains like "a - b - c" left-associative: the
// recursive call for the right-hand side stops at the second "-" instead
// of absorbing it, so the outer loop picks it up and combines
// left-to-right.
func (p *Parser) parseBinaryExpr(minPrec int) ast.Expr {
	left := p.parsePrimary()
	for {
		prec := precedenceOf(p.cur.Kind)
		if prec <= minPrec {
			return left
		}
		op := p.cur.Kind
		pos := p.cur.Pos
		p.next()
		right := p.parseBinaryExpr(prec)
		left = &ast.BinaryOp{Op: op, Left: left, Right: right, Pos: pos}
	}
}

// parsePrimary := INT | DOUBLE | STRING | "true" | "false"
//
//	| "." IDENT | "(" Expr ")" | RecordExpr | IDENT "(" Args ")"
func (p *Parser) parsePrimary() ast.Expr {
	pos := p.cur.Pos
	switch p.cur.Kind {
	case lexer.ILLEGAL:
		p.fail(pos, "%s", p.cur.Lit)
		return nil

	case lexer.INT:
		lit := p.next().Lit
		v, err := strconv.ParseInt(lit, 10, 64)
		if err != nil {
			p.fail(pos, "invalid integer literal %q", lit)
		}
		return &ast.IntLit{Value: v, Pos: pos}

	case lexer.DOUBLE:
		lit := p.next().Lit
		v, err := strconv.ParseFloat(lit, 64)
		if err != nil {
			p.fail(pos, "invalid double literal %q", lit)
		}
		return &ast.DoubleLit{Value: v, Pos: pos}

	case lexer.STRING:
		return &ast.StringLit{Value: p.next().Lit, Pos: pos}

	case lexer.TRUE:
		p.next()
		return &ast.BoolLit{Value: true, Pos: pos}

	case lexer.FALSE:
		p.next()
		return &ast.BoolLit{Value: false, Pos: pos}

	case lexer.DOT:
		p.next()
		return &ast.FieldAccess{Field: p.expectIdent(), Pos: pos}

	case lexer.LPAREN:
		p.next()
		inner := p.parseExpr()
		p.expect(lexer.RPAREN)
		return inner

	case lexer.LBRACE:
		return p.parseRecordExpr()

	case lexer.IDENT:
		return p.parseCallOrParamRef(pos)

	default:
		p.fail(pos, "unexpected %s in expression", p.cur)
		return nil
	}
}

// parseCallOrParamRef := IDENT "(" (Expr ("," Expr)*)? ")" | IDENT
//
// A bare identifier followed by "(" is a function call, same as always.
// One with no parens used to be a flat parse error in v0 ("no variable
// bindings to reference"). design-segments.md §2.2 adds exactly one:
// a scalar parameter's bare name inside its own segment's body ("min" in
// `filter(.age >= min)`). The parser can't tell that apart from a typo --
// it needs the enclosing segment's parameter list, checker business --
// so every bare identifier now parses as an ast.ParamRef, and the
// checker's monomorphization pass either substitutes it with the call
// site's literal argument or rejects it as an undefined name.
func (p *Parser) parseCallOrParamRef(pos lexer.Pos) ast.Expr {
	name := p.next().Lit
	if p.cur.Kind != lexer.LPAREN {
		return &ast.ParamRef{Name: name, Pos: pos}
	}
	p.next()
	var args []ast.Expr
	if p.cur.Kind != lexer.RPAREN {
		args = append(args, p.parseExpr())
		for p.cur.Kind == lexer.COMMA {
			p.next()
			args = append(args, p.parseExpr())
		}
	}
	p.expect(lexer.RPAREN)
	return &ast.Call{Fn: name, Args: args, Pos: pos}
}
