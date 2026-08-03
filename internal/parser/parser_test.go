package parser

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
)

// exprString renders an expression as a fully-parenthesized s-expression
// so precedence/associativity tests can compare against a short, exact
// string instead of hand-building and reflect.DeepEqual-ing a tree.
func exprString(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.IntLit:
		return strconv.FormatInt(e.Value, 10)
	case *ast.DoubleLit:
		return strconv.FormatFloat(e.Value, 'g', -1, 64)
	case *ast.StringLit:
		return strconv.Quote(e.Value)
	case *ast.BoolLit:
		return strconv.FormatBool(e.Value)
	case *ast.FieldAccess:
		return "." + e.Field
	case *ast.BinaryOp:
		return fmt.Sprintf("(%s %s %s)", exprString(e.Left), e.Op, exprString(e.Right))
	case *ast.Call:
		parts := make([]string, len(e.Args))
		for i, a := range e.Args {
			parts[i] = exprString(a)
		}
		return e.Fn + "(" + strings.Join(parts, ", ") + ")"
	default:
		return fmt.Sprintf("%#v", e)
	}
}

func TestExprPrecedenceAndAssociativity(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{"1 + 2 * 3", "(1 PLUS (2 STAR 3))"},
		{"1 * 2 + 3", "((1 STAR 2) PLUS 3)"},
		{"1 - 2 - 3", "((1 MINUS 2) MINUS 3)"},
		{"1 - 2 + 3", "((1 MINUS 2) PLUS 3)"},
		{"true || false && true", "(true OR (false AND true))"},
		{"(1 + 2) * 3", "((1 PLUS 2) STAR 3)"},
		{".age >= 18 && .active == true", "((.age GE 18) AND (.active EQ true))"},
		{"1 != 2 == false", "((1 NE 2) EQ false)"},
		{"1 < 2", "(1 LT 2)"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			expr, err := ParseExpr(tt.src)
			if err != nil {
				t.Fatalf("ParseExpr(%q) error: %v", tt.src, err)
			}
			if got := exprString(expr); got != tt.want {
				t.Errorf("ParseExpr(%q) = %s, want %s", tt.src, got, tt.want)
			}
		})
	}
}

func TestExprCallsAndNesting(t *testing.T) {
	expr, err := ParseExpr(`lower(trim(.email))`)
	if err != nil {
		t.Fatalf("ParseExpr error: %v", err)
	}
	want := `lower(trim(.email))`
	if got := exprString(expr); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// TestParseAdultsFilter parses design.md §7 Case A's adults.sift
// end to end and checks the resulting tree against the shape module 4
// hand-built -- this is the parser actually producing it now instead.
func TestParseAdultsFilter(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("adults.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> out
}`

	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if len(prog.Sources) != 1 {
		t.Fatalf("Sources = %d, want 1", len(prog.Sources))
	}
	src0 := prog.Sources[0]
	if src0.Name != "in" || src0.Format != "csv" || src0.Path != "people.csv" {
		t.Errorf("source = %+v, want {Name: in, Format: csv, Path: people.csv}", src0)
	}
	if src0.Pos != (lexer.Pos{Line: 1, Col: 1}) {
		t.Errorf("source Pos = %v, want 1:1", src0.Pos)
	}
	wantSchema := []ast.SchemaField{
		{Name: "name", TypeName: "string"},
		{Name: "age", TypeName: "int"},
	}
	for i, want := range wantSchema {
		got := src0.Schema.Fields[i]
		if got.Name != want.Name || got.TypeName != want.TypeName || got.PII != want.PII {
			t.Errorf("schema field %d = %+v, want %+v", i, got, want)
		}
	}

	if len(prog.Sinks) != 1 {
		t.Fatalf("Sinks = %d, want 1", len(prog.Sinks))
	}
	sink0 := prog.Sinks[0]
	if sink0.Name != "out" || sink0.Format != "jsonl" || sink0.Path != "adults.jsonl" {
		t.Errorf("sink = %+v, want {Name: out, Format: jsonl, Path: adults.jsonl}", sink0)
	}
	if sink0.Pos != (lexer.Pos{Line: 2, Col: 1}) {
		t.Errorf("sink Pos = %v, want 2:1", sink0.Pos)
	}

	if len(prog.Pipelines) != 1 {
		t.Fatalf("Pipelines = %d, want 1", len(prog.Pipelines))
	}
	main := prog.Pipelines[0]
	if main.Name != "main" {
		t.Errorf("pipeline name = %q, want main", main.Name)
	}
	if len(main.Body) != 3 {
		t.Fatalf("pipeline body has %d stages, want 3", len(main.Body))
	}
	if ref, ok := main.Body[0].(*ast.NameRef); !ok || ref.Name != "in" {
		t.Errorf("body[0] = %#v, want NameRef{in}", main.Body[0])
	}
	filter, ok := main.Body[1].(*ast.Filter)
	if !ok {
		t.Fatalf("body[1] = %#v, want *ast.Filter", main.Body[1])
	}
	if want := "(.age GE 18)"; exprString(filter.Pred) != want {
		t.Errorf("filter.Pred = %s, want %s", exprString(filter.Pred), want)
	}
	if ref, ok := main.Body[2].(*ast.NameRef); !ok || ref.Name != "out" {
		t.Errorf("body[2] = %#v, want NameRef{out}", main.Body[2])
	}
}

// TestParseMultiSinkTerminalList covers design-multisink.md §2: after
// the final "|>", a comma-separated list of sink NameRefs becomes
// multiple trailing elements in the pipeline body, in declared order.
func TestParseMultiSinkTerminalList(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string })
sink out = jsonl("out.jsonl")
sink out2 = jsonl("out2.jsonl")
sink out3 = jsonl("out3.jsonl")

pipeline main {
  in |> out, out2, out3
}`

	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	main := prog.Pipelines[0]
	if len(main.Body) != 4 {
		t.Fatalf("pipeline body has %d stages, want 4 (source + 3 sinks)", len(main.Body))
	}
	wantNames := []string{"in", "out", "out2", "out3"}
	for i, want := range wantNames {
		ref, ok := main.Body[i].(*ast.NameRef)
		if !ok || ref.Name != want {
			t.Errorf("body[%d] = %#v, want NameRef{%s}", i, main.Body[i], want)
		}
	}
}

// TestParseSingleSinkIsOneElementCase confirms the pre-multisink grammar
// still parses unchanged: a single sink with no trailing comma is just
// the one-element case of the same terminal production.
func TestParseSingleSinkIsOneElementCase(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> out
}`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	main := prog.Pipelines[0]
	if len(main.Body) != 2 {
		t.Fatalf("pipeline body has %d stages, want 2", len(main.Body))
	}
}

// TestParsePIIDeclassify covers design.md §7 Case B's shape: a @pii
// schema field, and a map stage that clears it with mask().
func TestParsePIIDeclassify(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> map({ ...row, email: mask(.email) }) |> out
}`

	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	emailField := prog.Sources[0].Schema.Fields[1]
	if emailField.Name != "email" || !emailField.PII {
		t.Fatalf("email field = %+v, want PII tagged", emailField)
	}

	m, ok := prog.Pipelines[0].Body[1].(*ast.Map)
	if !ok {
		t.Fatalf("body[1] = %#v, want *ast.Map", prog.Pipelines[0].Body[1])
	}
	if m.Record.Spread != "row" {
		t.Errorf("Record.Spread = %q, want %q", m.Record.Spread, "row")
	}
	if len(m.Record.Fields) != 1 || m.Record.Fields[0].Name != "email" {
		t.Fatalf("Record.Fields = %+v, want one field named email", m.Record.Fields)
	}
	if want := "mask(.email)"; exprString(m.Record.Fields[0].Value) != want {
		t.Errorf("email value = %s, want %s", exprString(m.Record.Fields[0].Value), want)
	}
}

// TestParseNamedSegmentEqualsForm covers design.md §2's named-segment
// syntax (`pipeline clean = ...`, no braces) alongside the brace form,
// and a NameRef to a named segment used inside another pipeline's body.
func TestParseNamedSegmentEqualsForm(t *testing.T) {
	const src = `pipeline clean =
     check(.email != "", "missing email")
  |> map({ ...row, email: lower(trim(.email)) })

pipeline main {
  in |> clean |> out
}`

	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(prog.Pipelines) != 2 {
		t.Fatalf("Pipelines = %d, want 2", len(prog.Pipelines))
	}

	clean := prog.Pipelines[0]
	if clean.Name != "clean" || len(clean.Body) != 2 {
		t.Fatalf("clean = %+v, want Name=clean with 2 stages", clean)
	}
	check, ok := clean.Body[0].(*ast.Check)
	if !ok {
		t.Fatalf("clean.Body[0] = %#v, want *ast.Check", clean.Body[0])
	}
	if check.Reason != "missing email" {
		t.Errorf("Reason = %q, want %q", check.Reason, "missing email")
	}
	if want := `(.email NE "")`; exprString(check.Cond) != want {
		t.Errorf("Cond = %s, want %s", exprString(check.Cond), want)
	}
	m, ok := clean.Body[1].(*ast.Map)
	if !ok {
		t.Fatalf("clean.Body[1] = %#v, want *ast.Map", clean.Body[1])
	}
	if want := "lower(trim(.email))"; exprString(m.Record.Fields[0].Value) != want {
		t.Errorf("map value = %s, want %s", exprString(m.Record.Fields[0].Value), want)
	}

	main := prog.Pipelines[1]
	if len(main.Body) != 3 {
		t.Fatalf("main body = %d stages, want 3", len(main.Body))
	}
	if ref, ok := main.Body[1].(*ast.NameRef); !ok || ref.Name != "clean" {
		t.Errorf("main.Body[1] = %#v, want NameRef{clean}", main.Body[1])
	}
}

func TestParseRecordExprShapes(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want *ast.RecordExpr
	}{
		{
			name: "spread only",
			src:  `{ ...row }`,
			want: &ast.RecordExpr{Spread: "row"},
		},
		{
			name: "spread and fields",
			src:  `{ ...row, name: .name }`,
			want: &ast.RecordExpr{Spread: "row", Fields: []ast.RecordField{{Name: "name"}}},
		},
		{
			name: "no spread",
			src:  `{ adult: .age >= 18 }`,
			want: &ast.RecordExpr{Fields: []ast.RecordField{{Name: "adult"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(tt.src)
			got := p.parseRecordExpr()
			if got.Spread != tt.want.Spread {
				t.Errorf("Spread = %q, want %q", got.Spread, tt.want.Spread)
			}
			if len(got.Fields) != len(tt.want.Fields) {
				t.Fatalf("Fields = %d, want %d", len(got.Fields), len(tt.want.Fields))
			}
			for i, f := range tt.want.Fields {
				if got.Fields[i].Name != f.Name {
					t.Errorf("Fields[%d].Name = %q, want %q", i, got.Fields[i].Name, f.Name)
				}
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantSub string
	}{
		{"garbage at top level", `42`, "expected a source, sink, pipeline, or error-policy declaration"},
		{"missing comma before schema", `source in = csv("x.csv" schema: {})`, "expected RPAREN"},
		{"field access as segment call argument", `pipeline main { in |> foo(.x) |> out }`, "not a field access"},
		{"unknown tag", `source in = csv("x.csv", schema: { name: string @xyz })`, `@pii`},
		{"unterminated string", `source in = csv("x.csv`, "unterminated string literal"},
		{"bad pipeline separator", `pipeline main : in`, "expected '(', '{', or '='"},
		{"comma before any pipe", `pipeline main { in, out }`, "expected RBRACE, got COMMA"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.src)
			if err == nil {
				t.Fatalf("Parse(%q) succeeded, want error containing %q", tt.src, tt.wantSub)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("Parse(%q) error = %q, want substring %q", tt.src, err.Error(), tt.wantSub)
			}
		})
	}
}

func TestParseExprErrors(t *testing.T) {
	if _, err := ParseExpr("1 2"); err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Errorf("ParseExpr(%q) error = %v, want an 'unexpected' error", "1 2", err)
	}
}

// TestParseErrorPositions spot-checks that a ParseError's Pos actually
// points at the offending token, not just somewhere in the file.
func TestParseErrorPositions(t *testing.T) {
	// The stray '#' is on line 2.
	src := "source in = csv(\"x.csv\", schema: {})\n#"
	_, err := Parse(src)
	if err == nil {
		t.Fatal("expected an error")
	}
	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("error type = %T, want *ParseError", err)
	}
	if pe.Pos.Line != 2 {
		t.Errorf("Pos.Line = %d, want 2", pe.Pos.Line)
	}
}
