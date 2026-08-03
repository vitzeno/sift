package parser

import (
	"strconv"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
)

// parseStageChain := StageElem ( "|>" StageElem )*
func (p *Parser) parseStageChain() []ast.Stage {
	stages := []ast.Stage{p.parseStageElem()}
	for p.cur.Kind == lexer.PIPE {
		p.next()
		stages = append(stages, p.parseStageElem())
	}
	return stages
}

// parseStageElem := IDENT | "filter" "(" Expr ")" | "map" "(" RecordExpr ")"
//
//	| "check" "(" Expr "," STRING ")"
//	| "select" "(" ColumnRefList ")" | "drop" "(" ColumnRefList ")"
//	| "rename" "(" RenamePair ("," RenamePair)* ")"
//	| "limit" "(" INT ")" | "offset" "(" INT ")"
//
// decision: built-in stage names are recognized by their literal text
// here in the parser, not as lexer keywords (module 3's decision).
// They're not a pluggable set like formats -- v0's stage grammar is
// closed to a fixed list (ast.BuiltinStageNames), each with its own
// argument shape, so the parser has to know their names to know how to
// parse what follows the '('. A bare identifier with no following '(' is
// a NameRef to a declared source, sink, or named pipeline segment;
// resolving which is the checker's job.
func (p *Parser) parseStageElem() ast.Stage {
	pos := p.cur.Pos
	if p.cur.Kind == lexer.ILLEGAL {
		p.fail(pos, "%s", p.cur.Lit)
	}
	if p.cur.Kind == lexer.SKIP {
		// "skip" is already a lexer keyword (the error-policy grammar's
		// `on error skip`, design-errors.md §3.1), so it can never reach
		// expectIdent() below as a plain identifier at all — intercept
		// it here with a specific, helpful redirect instead of the
		// generic "expected IDENT, got SKIP". This is exactly the
		// collision design-improvements.md §3 cites for locking the
		// slicing stage's name as "offset", not "skip".
		p.fail(pos, "%q is reserved for the error policy; did you mean %q?", "skip", "offset")
	}
	name := p.expectIdent()
	if p.cur.Kind != lexer.LPAREN {
		return &ast.NameRef{Name: name, Pos: pos}
	}
	switch name {
	case "filter":
		return p.parseFilter(pos)
	case "map":
		return p.parseMap(pos)
	case "check":
		return p.parseCheck(pos)
	case "select":
		return p.parseSelect(pos)
	case "drop":
		return p.parseDrop(pos)
	case "rename":
		return p.parseRename(pos)
	case "limit":
		return p.parseLimit(pos)
	case "offset":
		return p.parseOffset(pos)
	case "take":
		// decision: a specific, helpful rejection rather than falling
		// through to the generic "unknown stage" message
		// (design-improvements.md §3 locks the name as "limit", not
		// "take" — a likely guess from other languages/tools).
		p.fail(pos, "unknown stage %q (did you mean %q?)", "take", "limit")
		return nil
	default:
		p.fail(pos, "unknown stage %q (built-in stages are filter, map, check, select, drop, rename, limit, offset)", name)
		return nil
	}
}

func (p *Parser) parseFilter(pos lexer.Pos) *ast.Filter {
	p.expect(lexer.LPAREN)
	pred := p.parseExpr()
	p.expect(lexer.RPAREN)
	return &ast.Filter{Pred: pred, Pos: pos}
}

func (p *Parser) parseMap(pos lexer.Pos) *ast.Map {
	p.expect(lexer.LPAREN)
	rec := p.parseRecordExpr()
	p.expect(lexer.RPAREN)
	return &ast.Map{Record: rec, Pos: pos}
}

func (p *Parser) parseCheck(pos lexer.Pos) *ast.Check {
	p.expect(lexer.LPAREN)
	cond := p.parseExpr()
	p.expect(lexer.COMMA)
	reason := p.expectString()
	p.expect(lexer.RPAREN)
	return &ast.Check{Cond: cond, Reason: reason, Pos: pos}
}

func (p *Parser) parseSelect(pos lexer.Pos) *ast.Select {
	p.expect(lexer.LPAREN)
	cols := p.parseColumnRefList()
	p.expect(lexer.RPAREN)
	return &ast.Select{Columns: cols, Pos: pos}
}

func (p *Parser) parseDrop(pos lexer.Pos) *ast.Drop {
	p.expect(lexer.LPAREN)
	cols := p.parseColumnRefList()
	p.expect(lexer.RPAREN)
	return &ast.Drop{Columns: cols, Pos: pos}
}

func (p *Parser) parseRename(pos lexer.Pos) *ast.Rename {
	p.expect(lexer.LPAREN)
	pairs := []ast.RenamePair{p.parseRenamePair()}
	for p.cur.Kind == lexer.COMMA {
		p.next()
		pairs = append(pairs, p.parseRenamePair())
	}
	p.expect(lexer.RPAREN)
	return &ast.Rename{Pairs: pairs, Pos: pos}
}

// parseRenamePair := ColumnRef ":" ColumnRef  (old ":" new)
func (p *Parser) parseRenamePair() ast.RenamePair {
	pos := p.cur.Pos
	old := p.parseColumnRef()
	p.expect(lexer.COLON)
	new := p.parseColumnRef()
	return ast.RenamePair{Old: old.Name, New: new.Name, Pos: pos}
}

func (p *Parser) parseLimit(pos lexer.Pos) *ast.Limit {
	p.expect(lexer.LPAREN)
	n := p.parseIntLiteralArg()
	p.expect(lexer.RPAREN)
	return &ast.Limit{N: n, Pos: pos}
}

func (p *Parser) parseOffset(pos lexer.Pos) *ast.Offset {
	p.expect(lexer.LPAREN)
	n := p.parseIntLiteralArg()
	p.expect(lexer.RPAREN)
	return &ast.Offset{N: n, Pos: pos}
}

// parseIntLiteralArg consumes a bare int literal. limit/offset take a
// compile-time constant, not a general expression
// (design-improvements.md §3), so this reads one INT token directly
// rather than calling parseExpr — the same conversion parsePrimary
// already does for an INT literal in expression position.
func (p *Parser) parseIntLiteralArg() int64 {
	tok := p.expect(lexer.INT)
	n, err := strconv.ParseInt(tok.Lit, 10, 64)
	if err != nil {
		p.fail(tok.Pos, "invalid integer literal %q", tok.Lit)
	}
	return n
}

// parseColumnRefList := ColumnRef ("," ColumnRef)*
//
// Always at least one: the grammar has no way to write an empty list,
// so `select()` fails here with a plain "expected IDENT" parse error
// rather than needing a dedicated "zero names" check later.
func (p *Parser) parseColumnRefList() []ast.ColumnRef {
	cols := []ast.ColumnRef{p.parseColumnRef()}
	for p.cur.Kind == lexer.COMMA {
		p.next()
		cols = append(cols, p.parseColumnRef())
	}
	return cols
}

// parseColumnRef consumes a bare column-name identifier — the argument
// form select/drop/rename/mask/hash/redact all share
// (design-improvements.md §5). Writing `.field` here is a specific,
// anticipated mistake (the expression form, valid inside
// filter/map/check but not here), so it gets its own diagnostic instead
// of a generic "expected IDENT, got DOT".
func (p *Parser) parseColumnRef() ast.ColumnRef {
	pos := p.cur.Pos
	if p.cur.Kind == lexer.DOT {
		p.next()
		name := p.expectIdent()
		p.fail(pos, "expected a column name %q, not a field access %q", name, "."+name)
	}
	name := p.expectIdent()
	return ast.ColumnRef{Name: name, Pos: pos}
}

// parseRecordExpr := "{" ("..." IDENT ","?)? (RecordField ("," RecordField)*)? "}"
func (p *Parser) parseRecordExpr() *ast.RecordExpr {
	pos := p.cur.Pos
	p.expect(lexer.LBRACE)
	rec := &ast.RecordExpr{Pos: pos}

	if p.cur.Kind == lexer.ELLIPSIS {
		p.next()
		rec.Spread = p.expectIdent()
		if p.cur.Kind == lexer.COMMA {
			p.next()
		}
	}
	for p.cur.Kind != lexer.RBRACE {
		rec.Fields = append(rec.Fields, p.parseRecordField())
		if p.cur.Kind != lexer.COMMA {
			break
		}
		p.next()
	}
	p.expect(lexer.RBRACE)
	return rec
}

// parseRecordField := IDENT ":" Expr
func (p *Parser) parseRecordField() ast.RecordField {
	pos := p.cur.Pos
	name := p.expectIdent()
	p.expect(lexer.COLON)
	val := p.parseExpr()
	return ast.RecordField{Name: name, Value: val, Pos: pos}
}
