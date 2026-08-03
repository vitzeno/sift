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
			// structural constraint — no namespace/type resolution is
			// needed to see a second one coming — so it's rejected
			// here, in the parser, rather than deferred to the checker
			// the way "more than one runnable pipeline" is (that check
			// genuinely needs the namespace built first).
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

// parseSourceDecl := "source" IDENT "=" IDENT "(" STRING "," "schema" ":" SchemaLit ")"
func (p *Parser) parseSourceDecl() *ast.SourceDecl {
	pos := p.cur.Pos
	p.expect(lexer.SOURCE)
	name := p.expectIdent()
	p.expect(lexer.ASSIGN)
	format := p.expectIdent()
	p.expect(lexer.LPAREN)
	path := p.expectString()
	p.expect(lexer.COMMA)
	p.expectKeyword("schema")
	p.expect(lexer.COLON)
	schema := p.parseSchemaLit()
	p.expect(lexer.RPAREN)
	return &ast.SourceDecl{Name: name, Format: format, Path: path, Schema: schema, Pos: pos}
}

// parseSinkDecl := "sink" IDENT "=" IDENT "(" STRING ")"
//
// No schema keyword arg here: design.md §3 declares schema only on
// sources; a sink's expected schema is whatever the checker computes for
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

// parseSchemaField := IDENT ":" IDENT ("@" IDENT)?
//
// decision: @pii is the only tag v0 recognizes. Rather than building a
// general attribute grammar for a single case, the parser accepts `@`
// followed by any identifier and rejects anything but "pii" outright --
// simpler, and the error message is exactly as useful either way.
func (p *Parser) parseSchemaField() ast.SchemaField {
	pos := p.cur.Pos
	name := p.expectIdent()
	p.expect(lexer.COLON)
	typeName := p.expectIdent()
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
	return ast.SchemaField{Name: name, TypeName: typeName, PII: pii, Pos: pos}
}

// parsePipelineDecl := "pipeline" IDENT ( "{" StageChain "}" | "=" StageChain )
func (p *Parser) parsePipelineDecl() *ast.PipelineDecl {
	pos := p.cur.Pos
	p.expect(lexer.PIPELINE)
	name := p.expectIdent()

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
		p.fail(p.cur.Pos, "expected '{' or '=' after pipeline name, got %s", p.cur)
	}
	return &ast.PipelineDecl{Name: name, Body: body, Pos: pos}
}
