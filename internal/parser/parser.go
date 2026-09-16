// Package parser turns a lexer.Token stream into an *ast.Program:
// recursive descent for top-level and pipeline structure, a Pratt
// (precedence-climbing) parser for expressions.
package parser

import (
	"fmt"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
)

// ParseError is a parser diagnostic: a position plus a message, phrased
// to read like a data tool rather than a stack trace. The CLI prefixes
// it with the source file name; the parser itself doesn't know what file
// it's reading.
type ParseError struct {
	Pos lexer.Pos
	Msg string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s: %s", e.Pos, e.Msg)
}

// Parser holds a two-token lookahead window over a lexer.
type Parser struct {
	lex  *lexer.Lexer
	cur  lexer.Token
	peek lexer.Token
}

func newParser(src string) *Parser {
	p := &Parser{lex: lexer.New(src)}
	p.cur = p.lex.Next()
	p.peek = p.lex.Next()
	return p
}

// abort carries a *ParseError through a panic. This is an internal
// bailout, not a user-facing panic: Parse and ParseExpr are the only two
// entry points into this package, and both recover it and return a plain
// error, so nothing outside this package ever sees a panic. The
// alternative, threading `if
// err != nil { return }` through every one of the ~20 parse* functions
// below, would obscure the grammar those functions are meant to read like.
type abort struct{ err *ParseError }

func (p *Parser) fail(pos lexer.Pos, format string, args ...any) {
	panic(abort{&ParseError{Pos: pos, Msg: fmt.Sprintf(format, args...)}})
}

// recoverErr is deferred by every exported entry point to turn an abort
// panic into a returned error. Any other panic is a real bug in the
// parser and is left to propagate rather than get silently swallowed.
func recoverErr(err *error) {
	if r := recover(); r != nil {
		if a, ok := r.(abort); ok {
			*err = a.err
			return
		}
		panic(r)
	}
}

func (p *Parser) next() lexer.Token {
	tok := p.cur
	p.cur = p.peek
	p.peek = p.lex.Next()
	return tok
}

// expect consumes the current token if it has the given kind, or fails.
// An ILLEGAL current token always fails first, so the error is the
// lexer's own message (e.g. "unterminated string literal") instead of a
// confusing "expected X, got ILLEGAL".
func (p *Parser) expect(kind lexer.Kind) lexer.Token {
	if p.cur.Kind == lexer.ILLEGAL {
		p.fail(p.cur.Pos, "%s", p.cur.Lit)
	}
	if p.cur.Kind != kind {
		p.fail(p.cur.Pos, "expected %s, got %s", kind, p.cur)
	}
	return p.next()
}

func (p *Parser) expectIdent() string {
	return p.expect(lexer.IDENT).Lit
}

func (p *Parser) expectString() string {
	return p.expect(lexer.STRING).Lit
}

// Parse parses a complete .sift program.
func Parse(src string) (prog *ast.Program, err error) {
	defer recoverErr(&err)
	p := newParser(src)
	prog = p.parseProgram()
	return prog, nil
}

// ParseExpr parses a single standalone expression and requires the input
// be fully consumed. It exists so the Pratt parser (precedence,
// associativity, primaries) can be tested directly, without wrapping
// every case in a full source/pipeline declaration.
func ParseExpr(src string) (expr ast.Expr, err error) {
	defer recoverErr(&err)
	p := newParser(src)
	expr = p.parseExpr()
	if p.cur.Kind != lexer.EOF {
		p.fail(p.cur.Pos, "unexpected %s after expression", p.cur)
	}
	return expr, nil
}
