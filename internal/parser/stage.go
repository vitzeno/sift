package parser

import (
	"strconv"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
)

// parseStageChain := StageElem ( "|>" StageElem )* ( "," IDENT )*
//
// The trailing comma-list is design-multisink.md §2's terminal
// broadcast: after the final "|>", the terminal production may be a
// comma-separated list of sink NameRefs (`|> out, out_2`), not just one.
// A single sink is the one-element case, so existing programs parse
// unchanged. It's checked here only if the chain's last element parsed
// as a bare NameRef, the only shape a trailing name can take since every
// built-in stage requires "(". A top-level comma is not otherwise valid
// in stage-chain position, so this is unambiguous with no new lexer
// token.
//
// A route terminal (design-routing.md §7) needs no special handling
// here: parseStageElem returns an *ast.RouteTerminal, not a NameRef, so
// the broadcast-comma check above just doesn't apply to it, and
// parseRouteTerminal itself rejects a trailing "|>". The two terminal
// productions stay mutually exclusive by construction.
func (p *Parser) parseStageChain() []ast.Stage {
	stages := []ast.Stage{p.parseStageElem()}
	for p.cur.Kind == lexer.PIPE {
		p.next()
		stages = append(stages, p.parseStageElem())
	}
	// len(stages) > 1 guards against treating the leading source ref
	// itself as the start of a sink list if it's followed by a stray
	// comma with no "|>" ever consumed (e.g. a malformed `in, out`). The
	// comma-list only continues a chain that already reached its
	// terminal "|>".
	if _, ok := stages[len(stages)-1].(*ast.NameRef); ok && len(stages) > 1 {
		for p.cur.Kind == lexer.COMMA {
			p.next()
			pos := p.cur.Pos
			name := p.expectIdent()
			stages = append(stages, &ast.NameRef{Name: name, Pos: pos})
		}
	}
	return stages
}

// parseStageElem := IDENT | "filter" "(" Expr ")" | "map" "(" RecordExpr ")"
//
//	| "check" "(" Expr "," STRING ")"
//	| "select" "(" ColumnRefList ")" | "drop" "(" ColumnRefList ")"
//	| "rename" "(" RenamePair ("," RenamePair)* ")"
//	| "limit" "(" INT ")" | "offset" "(" INT ")"
//	| ("mask" | "hash" | "redact") "(" ColumnRefList ")"
//	| IDENT "(" CallArgList ")"
//
// decision: built-in stage names are recognized by their literal text
// here in the parser, not as lexer keywords (module 3's decision).
// They're not a pluggable set like formats: v0's stage grammar is closed
// to a fixed list (ast.BuiltinStageNames), each with its own argument
// shape, so the parser has to know their names to know how to parse
// what follows the '('. A bare identifier with no following '(' is a
// NameRef to a declared source, sink, or named pipeline segment; one
// followed by '(' but not in the closed list is a parameterized segment
// call (design-segments.md §2). Resolving either is the checker's job.
func (p *Parser) parseStageElem() ast.Stage {
	pos := p.cur.Pos
	if p.cur.Kind == lexer.ILLEGAL {
		p.fail(pos, "%s", p.cur.Lit)
	}
	if p.cur.Kind == lexer.SKIP {
		// "skip" is already a lexer keyword (the error-policy grammar's
		// `on error skip`, design-errors.md §3.1), so it can never reach
		// expectIdent() below as a plain identifier. Intercept it here
		// with a specific, helpful redirect instead of the generic
		// "expected IDENT, got SKIP". This is the collision
		// design-improvements.md §3 cites for locking the slicing
		// stage's name as "offset", not "skip".
		p.fail(pos, "%q is reserved for the error policy; did you mean %q?", "skip", "offset")
	}
	name := p.expectIdent()
	if name == "route" && p.cur.Kind == lexer.LBRACE {
		return p.parseRouteTerminal(pos)
	}
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
	case "mask", "hash", "redact":
		return p.parseDeclassifyStage(name, pos)
	case "take":
		// decision: a specific, helpful rejection rather than falling
		// through to parsing it as a segment call (design-improvements.md
		// §3 locks the name as "limit", not "take", a likely guess from
		// other languages/tools).
		p.fail(pos, "unknown stage %q (did you mean %q?)", "take", "limit")
		return nil
	default:
		return p.parseSegmentCall(name, pos)
	}
}

// parseSegmentCall := IDENT "(" (CallArg ("," CallArg)*)? ")"
//
// Any identifier not in ast.BuiltinStageNames, followed by "(", parses as
// a call to a parameterized named segment (design-segments.md §2):
// `scrub(email)`, `adults(18)`. Whether name actually names a declared
// pipeline segment, and whether its parameter count and kinds match, is
// the checker's job (expandSegmentCall). The parser only knows the call
// shape, same division of labor as a bare NameRef.
func (p *Parser) parseSegmentCall(name string, pos lexer.Pos) *ast.SegmentCall {
	p.expect(lexer.LPAREN)
	var args []ast.CallArg
	if p.cur.Kind != lexer.RPAREN {
		args = append(args, p.parseCallArg())
		for p.cur.Kind == lexer.COMMA {
			p.next()
			args = append(args, p.parseCallArg())
		}
	}
	p.expect(lexer.RPAREN)
	return &ast.SegmentCall{Name: name, Args: args, Pos: pos}
}

// parseCallArg := IDENT | INT | DOUBLE | STRING | "true" | "false"
//
// A segment call argument is a bare column name or a scalar literal,
// design-segments.md §2's two forms, never a general expression: a call
// argument is never stream-dependent (§6's scope fence). A bare
// identifier is always taken as a column-name argument; v0 has no syntax
// for forwarding a scalar parameter by name at a call site.
func (p *Parser) parseCallArg() ast.CallArg {
	pos := p.cur.Pos
	switch p.cur.Kind {
	case lexer.IDENT:
		return ast.CallArg{Kind: ast.ArgColumn, Column: p.next().Lit, Pos: pos}
	case lexer.INT, lexer.DOUBLE, lexer.STRING, lexer.TRUE, lexer.FALSE:
		return ast.CallArg{Kind: ast.ArgScalar, Literal: p.parsePrimary(), Pos: pos}
	case lexer.DOT:
		p.next()
		field := p.expectIdent()
		p.fail(pos, "segment call arguments are a column name or literal, not a field access (%s)", "."+field)
		return ast.CallArg{}
	default:
		p.fail(pos, "expected a column name or literal argument, got %s", p.cur)
		return ast.CallArg{}
	}
}

// parseRouteTerminal := "route" "{" RouteBranch ("," RouteBranch)* "}"
//
// "route" is recognized contextually right here, at terminal position,
// by its literal text plus a following "{", not as a lexer keyword
// (design-routing.md §7: the 10-keyword closed set does not grow). The
// "{" lookahead disambiguates it from an ordinary NameRef to a sink or
// segment that happens to be named "route" (unusual, but not forbidden;
// only a named *segment* is reserved against the name, design-routing.md
// §7/§11, enforced in the checker's namespace build).
//
// A route terminal always ends the pipeline (design-routing.md §1: still
// terminal, still one write per row). A stray "|>" after its closing "}"
// is rejected here with a specific diagnostic rather than being handed
// back to parseStageChain's loop, which would otherwise try to parse
// another stage after it.
func (p *Parser) parseRouteTerminal(pos lexer.Pos) *ast.RouteTerminal {
	p.expect(lexer.LBRACE)
	var branches []ast.RouteBranch
	for p.cur.Kind != lexer.RBRACE {
		branches = append(branches, p.parseRouteBranch())
		if p.cur.Kind != lexer.COMMA {
			break
		}
		p.next()
	}
	p.expect(lexer.RBRACE)
	if p.cur.Kind == lexer.PIPE {
		p.fail(p.cur.Pos, "route terminates the pipeline; nothing can follow it")
	}
	return &ast.RouteTerminal{Branches: branches, Pos: pos}
}

// parseRouteBranch := (Expr | "else") "=>" (IDENT | "discard")
//
// "else" is checked by literal text before falling back to parseExpr.
// Like "route" above, it's contextual, not a keyword, so it would
// otherwise parse as an ordinary (and here always wrong) ast.ParamRef.
func (p *Parser) parseRouteBranch() ast.RouteBranch {
	pos := p.cur.Pos
	if p.cur.Kind == lexer.IDENT && p.cur.Lit == "else" {
		p.next()
		p.expect(lexer.ARROW)
		target, discard := p.parseRouteTarget()
		return ast.RouteBranch{IsElse: true, Target: target, Discard: discard, Pos: pos}
	}
	pred := p.parseExpr()
	p.expect(lexer.ARROW)
	target, discard := p.parseRouteTarget()
	return ast.RouteBranch{Pred: pred, Target: target, Discard: discard, Pos: pos}
}

// parseRouteTarget := IDENT | "discard"
//
// "discard" (design-routing.md §2) is likewise contextual, recognized by
// literal text in exactly this one position, never a lexer keyword. It's
// never ambiguous with a real sink name since the checker resolves every
// non-discard target against the declared sink namespace regardless.
func (p *Parser) parseRouteTarget() (target *ast.NameRef, discard bool) {
	pos := p.cur.Pos
	name := p.expectIdent()
	if name == "discard" {
		return nil, true
	}
	return &ast.NameRef{Name: name, Pos: pos}, false
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
// rather than calling parseExpr, the same conversion parsePrimary
// already does for an INT literal in expression position.
func (p *Parser) parseIntLiteralArg() int64 {
	tok := p.expect(lexer.INT)
	n, err := strconv.ParseInt(tok.Lit, 10, 64)
	if err != nil {
		p.fail(tok.Pos, "invalid integer literal %q", tok.Lit)
	}
	return n
}

// parseDeclassifyStage parses mask/hash/redact in stage position, the
// same column-list grammar select/drop already use
// (design-improvements.md §4). fn is the already-consumed stage name,
// carried through unchanged so the checker/runtime know which
// declassifier to apply.
func (p *Parser) parseDeclassifyStage(fn string, pos lexer.Pos) *ast.Declassify {
	p.expect(lexer.LPAREN)
	cols := p.parseColumnRefList()
	p.expect(lexer.RPAREN)
	return &ast.Declassify{Fn: fn, Columns: cols, Pos: pos}
}

// parseColumnRefList := ColumnRef ("," ColumnRef)*
//
// Always at least one: the grammar has no way to write an empty list, so
// `select()` fails here with a plain "expected IDENT" parse error and
// needs no dedicated "zero names" check later.
func (p *Parser) parseColumnRefList() []ast.ColumnRef {
	cols := []ast.ColumnRef{p.parseColumnRef()}
	for p.cur.Kind == lexer.COMMA {
		p.next()
		cols = append(cols, p.parseColumnRef())
	}
	return cols
}

// parseColumnRef consumes a bare column-name identifier, the argument
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
