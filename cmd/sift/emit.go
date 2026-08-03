package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/checker"
	"github.com/vitzeno/sift/internal/parser"
)

// emitAST prints the parsed (not checked) AST: source/sink declarations
// as written, and each pipeline's `|>` chain one stage per line. It's a
// parser debugging aid, not a stable machine-readable format — v0 has no
// consumer for a serialized AST, so there's no format to keep compatible.
func emitAST(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	prog, err := parser.Parse(string(src))
	if err != nil {
		return err
	}

	for _, s := range prog.Sources {
		fmt.Printf("source %s = %s(%q, schema: %s%s)\n", s.Name, s.Format, s.Path, schemaLitString(s.Schema), sourceOptsString(s.Opts))
	}
	for _, s := range prog.Sinks {
		fmt.Printf("sink %s = %s(%q)\n", s.Name, s.Format, s.Path)
	}
	for _, p := range prog.Pipelines {
		fmt.Printf("pipeline %s:\n", p.Name)
		for _, st := range p.Body {
			fmt.Printf("  %s\n", stageString(st))
		}
	}
	return nil
}

// emitSchema prints the checked program's source and sink schemas, using
// value.Schema.String() directly — the whole reason module 1 gave Schema
// a String() method in the first place.
func emitSchema(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	prog, err := parser.Parse(string(src))
	if err != nil {
		return err
	}
	cp, err := checker.Check(prog)
	if err != nil {
		return err
	}

	fmt.Printf("source %s: %s\n", cp.Source.Name, cp.SourceSchema)
	for _, s := range cp.Sinks {
		fmt.Printf("sink %s: %s\n", s.Name, cp.SinkSchema)
	}
	return nil
}

func schemaLitString(s ast.SchemaLit) string {
	parts := make([]string, len(s.Fields))
	for i, f := range s.Fields {
		tag := ""
		if f.PII {
			tag = " @pii"
		}
		parts[i] = fmt.Sprintf("%s: %s%s", f.Name, f.TypeName, tag)
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// sourceOptsString renders a source's extra keyword arguments (beyond
// schema) as ", name: value" suffixes, in declared order, reusing
// exprString for the literal value — an xlsx source's `sheet:`/
// `header_row:` opts (design/xlsx.md §1) round-trip through --emit-ast
// exactly like schema does, rather than being silently dropped.
func sourceOptsString(opts []ast.SourceOpt) string {
	var b strings.Builder
	for _, o := range opts {
		fmt.Fprintf(&b, ", %s: %s", o.Name, exprString(o.Value))
	}
	return b.String()
}

func stageString(s ast.Stage) string {
	switch s := s.(type) {
	case *ast.NameRef:
		return s.Name
	case *ast.Filter:
		return fmt.Sprintf("filter(%s)", exprString(s.Pred))
	case *ast.Map:
		return fmt.Sprintf("map(%s)", exprString(s.Record))
	case *ast.Check:
		return fmt.Sprintf("check(%s, %q)", exprString(s.Cond), s.Reason)
	default:
		return fmt.Sprintf("%#v", s)
	}
}

func exprString(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.FieldAccess:
		return "." + e.Field
	case *ast.IntLit:
		return strconv.FormatInt(e.Value, 10)
	case *ast.DoubleLit:
		return strconv.FormatFloat(e.Value, 'g', -1, 64)
	case *ast.StringLit:
		return strconv.Quote(e.Value)
	case *ast.BoolLit:
		return strconv.FormatBool(e.Value)
	case *ast.BinaryOp:
		return fmt.Sprintf("(%s %s %s)", exprString(e.Left), e.Op.Symbol(), exprString(e.Right))
	case *ast.Call:
		parts := make([]string, len(e.Args))
		for i, a := range e.Args {
			parts[i] = exprString(a)
		}
		return e.Fn + "(" + strings.Join(parts, ", ") + ")"
	case *ast.RecordExpr:
		return recordString(e)
	default:
		return fmt.Sprintf("%#v", e)
	}
}

func recordString(r *ast.RecordExpr) string {
	var parts []string
	if r.Spread != "" {
		parts = append(parts, "..."+r.Spread)
	}
	for _, f := range r.Fields {
		parts = append(parts, f.Name+": "+exprString(f.Value))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}
