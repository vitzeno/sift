package parser

import (
	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
)

// parseProgram := (SourceDecl | SinkDecl | PipelineDecl | ErrorPolicyDecl)* EOF
func (p *Parser) parseProgram() *ast.Program {
	prog := &ast.Program{Pos: p.cur.Pos}
	for p.cur.Kind != lexer.EOF {
		switch p.cur.Kind {
		case lexer.SOURCE:
			prog.Sources = append(prog.Sources, p.parseSourceDecl())
		case lexer.SINK:
			prog.Sinks = append(prog.Sinks, p.parseSinkDecl())
		case lexer.PIPELINE:
			prog.Pipelines = append(prog.Pipelines, p.parsePipelineDecl())
		case lexer.ON:
			// decision: "at most one on error declaration" is a purely
			// structural check, no namespace or type resolution needed to
			// spot a second one, so it's rejected here in the parser
			// rather than deferred to the checker the way "more than one
			// runnable pipeline" is (that check genuinely needs the
			// namespace built first).
			if prog.ErrorPolicy != nil {
				p.fail(p.cur.Pos, "only one 'on error' declaration is allowed per program")
			}
			prog.ErrorPolicy = p.parseErrorPolicyDecl()
		case lexer.ILLEGAL:
			p.fail(p.cur.Pos, "%s", p.cur.Lit)
		default:
			p.fail(p.cur.Pos, "expected a source, sink, pipeline, or error-policy declaration, got %s", p.cur)
		}
	}
	return prog
}

// parseErrorPolicyDecl := "on" "error" ( "abort" | "skip" | "|>" IDENT )
func (p *Parser) parseErrorPolicyDecl() *ast.ErrorPolicyDecl {
	pos := p.cur.Pos
	p.expect(lexer.ON)
	p.expect(lexer.ERROR)

	switch p.cur.Kind {
	case lexer.ABORT:
		p.next()
		return &ast.ErrorPolicyDecl{Kind: ast.ErrorAbort, Pos: pos}
	case lexer.SKIP:
		p.next()
		return &ast.ErrorPolicyDecl{Kind: ast.ErrorSkip, Pos: pos}
	case lexer.PIPE:
		p.next()
		targetPos := p.cur.Pos
		name := p.expectIdent()
		return &ast.ErrorPolicyDecl{
			Kind:   ast.ErrorRoute,
			Target: &ast.NameRef{Name: name, Pos: targetPos},
			Pos:    pos,
		}
	case lexer.ILLEGAL:
		p.fail(p.cur.Pos, "%s", p.cur.Lit)
		return nil
	default:
		p.fail(p.cur.Pos, "expected 'abort', 'skip', or '|>' after 'on error', got %s", p.cur)
		return nil
	}
}

// parseSourceDecl := "source" IDENT "=" IDENT "(" STRING ("," SourceKwArg)* ")"
// SourceKwArg      := "schema" ":" SchemaLit | IDENT ":" Literal
//
// Exactly one "schema" kwarg is required, in any position among the
// kwargs. Every other kwarg is a format-specific option collected into
// ast.SourceDecl.Opts and left uninterpreted here (design/xlsx.md §1's
// "widen the generic kwarg handling", not an xlsx-specific grammar
// change per CLAUDE.md non-negotiable #2).
func (p *Parser) parseSourceDecl() *ast.SourceDecl {
	pos := p.cur.Pos
	p.expect(lexer.SOURCE)
	name := p.expectIdent()
	p.expect(lexer.ASSIGN)
	format := p.expectIdent()
	p.expect(lexer.LPAREN)
	path := p.expectString()

	var schema ast.SchemaLit
	haveSchema := false
	var opts []ast.SourceOpt
	for p.cur.Kind == lexer.COMMA {
		p.next()
		kwPos := p.cur.Pos
		kw := p.expectIdent()
		p.expect(lexer.COLON)
		if kw == "schema" {
			if haveSchema {
				p.fail(kwPos, "duplicate %q keyword argument", "schema")
			}
			schema = p.parseSchemaLit()
			haveSchema = true
			continue
		}
		opts = append(opts, ast.SourceOpt{Name: kw, Value: p.parseSourceOptValue(), Pos: kwPos})
	}
	p.expect(lexer.RPAREN)
	if !haveSchema {
		p.fail(pos, "source %q: missing required %q keyword argument", name, "schema")
	}
	return &ast.SourceDecl{Name: name, Format: format, Path: path, Schema: schema, Opts: opts, Pos: pos}
}

// parseSourceOptValue := INT | DOUBLE | STRING | "true" | "false"
//
// A source option's value is always a compile-time scalar literal, same
// as a segment call's scalar argument (parseCallArg): there's no row in
// scope at a source declaration, so a general expression would have
// nothing to evaluate against.
func (p *Parser) parseSourceOptValue() ast.Expr {
	pos := p.cur.Pos
	switch p.cur.Kind {
	case lexer.INT, lexer.DOUBLE, lexer.STRING, lexer.TRUE, lexer.FALSE:
		return p.parsePrimary()
	default:
		p.fail(pos, "expected a literal value for keyword argument, got %s", p.cur)
		return nil
	}
}

// parseSinkDecl := "sink" IDENT "=" IDENT "(" STRING ")"
//
// No schema keyword arg here: design.md §3 declares schema only on
// sources. A sink's expected schema is whatever the checker computes for
// the stream feeding it.
func (p *Parser) parseSinkDecl() *ast.SinkDecl {
	pos := p.cur.Pos
	p.expect(lexer.SINK)
	name := p.expectIdent()
	p.expect(lexer.ASSIGN)
	format := p.expectIdent()
	p.expect(lexer.LPAREN)
	path := p.expectString()
	p.expect(lexer.RPAREN)
	return &ast.SinkDecl{Name: name, Format: format, Path: path, Pos: pos}
}

// parseSchemaLit := "{" (SchemaField ("," SchemaField)*)? "}"
func (p *Parser) parseSchemaLit() ast.SchemaLit {
	pos := p.cur.Pos
	p.expect(lexer.LBRACE)
	var fields []ast.SchemaField
	for p.cur.Kind != lexer.RBRACE {
		fields = append(fields, p.parseSchemaField())
		if p.cur.Kind != lexer.COMMA {
			break
		}
		p.next()
	}
	p.expect(lexer.RBRACE)
	return ast.SchemaLit{Fields: fields, Pos: pos}
}

// parseSchemaField := IDENT ":" IDENT "?"? ("@" IDENT)?
//
// decision: @pii is the only tag v0 recognizes. Rather than build a
// general attribute grammar for one case, the parser accepts `@`
// followed by any identifier and rejects anything but "pii". Simpler,
// and the error message is just as useful either way.
func (p *Parser) parseSchemaField() ast.SchemaField {
	pos := p.cur.Pos
	name := p.expectIdent()
	p.expect(lexer.COLON)
	typeName := p.expectIdent()
	optional := false
	if p.cur.Kind == lexer.QUESTION {
		p.next()
		optional = true
	}
	pii := false
	if p.cur.Kind == lexer.AT {
		p.next()
		tagPos := p.cur.Pos
		tag := p.expectIdent()
		if tag != "pii" {
			p.fail(tagPos, "unknown type tag %q (only @pii is supported)", tag)
		}
		pii = true
	}
	return ast.SchemaField{Name: name, TypeName: typeName, Optional: optional, PII: pii, Pos: pos}
}

// parsePipelineDecl := "pipeline" IDENT ( "(" ParamList ")" )?
//
//	( "{" StageChain "}" | "=" StageChain )
func (p *Parser) parsePipelineDecl() *ast.PipelineDecl {
	pos := p.cur.Pos
	p.expect(lexer.PIPELINE)
	name := p.expectIdent()

	var params []ast.Param
	if p.cur.Kind == lexer.LPAREN {
		p.next()
		params = p.parseParamList()
		p.expect(lexer.RPAREN)
	}

	var body []ast.Stage
	switch p.cur.Kind {
	case lexer.LBRACE:
		p.next()
		body = p.parseStageChain()
		p.expect(lexer.RBRACE)
	case lexer.ASSIGN:
		p.next()
		body = p.parseStageChain()
	case lexer.ILLEGAL:
		p.fail(p.cur.Pos, "%s", p.cur.Lit)
	default:
		p.fail(p.cur.Pos, "expected '(', '{', or '=' after pipeline name, got %s", p.cur)
	}
	return &ast.PipelineDecl{Name: name, Params: params, Body: body, Pos: pos}
}

// parseParamList := Param ("," Param)*
func (p *Parser) parseParamList() []ast.Param {
	params := []ast.Param{p.parseParam()}
	for p.cur.Kind == lexer.COMMA {
		p.next()
		params = append(params, p.parseParam())
	}
	return params
}

// parseParam := IDENT (":" IDENT)?
//
// No colon: a column parameter (design-segments.md §2.1), referenced as
// `.name` inside the body and bound to a bare column name at the call
// site. With a colon: a scalar parameter (§2.2) of the named type,
// referenced as a bare value inside the body and bound to a literal at
// the call site. Like SchemaField.TypeName, the type name is left as raw
// identifier text; resolving it against int/double/string/bool is the
// checker's job, not the parser's.
func (p *Parser) parseParam() ast.Param {
	pos := p.cur.Pos
	name := p.expectIdent()
	if p.cur.Kind != lexer.COLON {
		return ast.Param{Name: name, Kind: ast.ParamColumn, Pos: pos}
	}
	p.next()
	typeName := p.expectIdent()
	return ast.Param{Name: name, Kind: ast.ParamScalar, TypeName: typeName, Pos: pos}
}
