package parser

import (
	"strings"
	"testing"

	"github.com/vitzeno/sift/internal/ast"
)

// TestParseScalarParam checks that a `name: type` parameter parses as
// ast.ParamScalar, and that its bare-name reference inside
// the body parses as an ast.ParamRef.
func TestParseScalarParam(t *testing.T) {
	prog, err := Parse(`pipeline adults(min: int) = filter(.age >= min)`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	seg := prog.Pipelines[0]
	if len(seg.Params) != 1 {
		t.Fatalf("Params = %+v, want 1 entry", seg.Params)
	}
	p := seg.Params[0]
	if p.Name != "min" || p.Kind != ast.ParamScalar || p.TypeName != "int" {
		t.Errorf("Params[0] = %+v, want {Name: min, Kind: ParamScalar, TypeName: int}", p)
	}

	filter, ok := seg.Body[0].(*ast.Filter)
	if !ok {
		t.Fatalf("Body[0] = %#v, want *ast.Filter", seg.Body[0])
	}
	bin, ok := filter.Pred.(*ast.BinaryOp)
	if !ok {
		t.Fatalf("Pred = %#v, want *ast.BinaryOp", filter.Pred)
	}
	ref, ok := bin.Right.(*ast.ParamRef)
	if !ok || ref.Name != "min" {
		t.Errorf("Pred.Right = %#v, want *ast.ParamRef{Name: min}", bin.Right)
	}
}

// TestParseColumnParam covers PS2: a bare-identifier parameter parses as
// ast.ParamColumn; inside the body it's referenced with ordinary
// field-access (`.col`) and column-name (bare `col`) syntax, exactly like
// a literal column would be. Substitution is a checker concern, not a
// parser one.
func TestParseColumnParam(t *testing.T) {
	prog, err := Parse(`pipeline scrub(col) = map({ ...row, col: mask(lower(trim(.col))) })`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	seg := prog.Pipelines[0]
	if len(seg.Params) != 1 {
		t.Fatalf("Params = %+v, want 1 entry", seg.Params)
	}
	p := seg.Params[0]
	if p.Name != "col" || p.Kind != ast.ParamColumn {
		t.Errorf("Params[0] = %+v, want {Name: col, Kind: ParamColumn}", p)
	}

	m, ok := seg.Body[0].(*ast.Map)
	if !ok {
		t.Fatalf("Body[0] = %#v, want *ast.Map", seg.Body[0])
	}
	if len(m.Record.Fields) != 1 || m.Record.Fields[0].Name != "col" {
		t.Fatalf("Record.Fields = %+v, want one field named %q", m.Record.Fields, "col")
	}
}

// TestParseMixedParams confirms column and scalar parameters can mix, in
// declared order.
func TestParseMixedParams(t *testing.T) {
	prog, err := Parse(`pipeline gate(col, min: int) = check(.col >= min, "below min")`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	params := prog.Pipelines[0].Params
	if len(params) != 2 {
		t.Fatalf("Params = %+v, want 2 entries", params)
	}
	if params[0].Kind != ast.ParamColumn || params[0].Name != "col" {
		t.Errorf("Params[0] = %+v, want {Name: col, Kind: ParamColumn}", params[0])
	}
	if params[1].Kind != ast.ParamScalar || params[1].Name != "min" || params[1].TypeName != "int" {
		t.Errorf("Params[1] = %+v, want {Name: min, Kind: ParamScalar, TypeName: int}", params[1])
	}
}

// TestParseSegmentCallSite covers PS3's syntax half: a call reads exactly
// like a built-in stage, with column arguments and scalar-literal
// arguments both parsing into ast.SegmentCall.
func TestParseSegmentCallSite(t *testing.T) {
	prog, err := Parse(`pipeline main {
  in |> scrub(email) |> adults(18) |> out
}`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	body := prog.Pipelines[0].Body

	scrub, ok := body[1].(*ast.SegmentCall)
	if !ok {
		t.Fatalf("Body[1] = %#v, want *ast.SegmentCall", body[1])
	}
	if scrub.Name != "scrub" || len(scrub.Args) != 1 || scrub.Args[0].Kind != ast.ArgColumn || scrub.Args[0].Column != "email" {
		t.Errorf("scrub call = %+v, want Name=scrub, Args=[{ArgColumn, email}]", scrub)
	}

	adults, ok := body[2].(*ast.SegmentCall)
	if !ok {
		t.Fatalf("Body[2] = %#v, want *ast.SegmentCall", body[2])
	}
	if adults.Name != "adults" || len(adults.Args) != 1 || adults.Args[0].Kind != ast.ArgScalar {
		t.Fatalf("adults call = %+v, want Name=adults, Args=[{ArgScalar, ...}]", adults)
	}
	lit, ok := adults.Args[0].Literal.(*ast.IntLit)
	if !ok || lit.Value != 18 {
		t.Errorf("adults arg literal = %#v, want *ast.IntLit{Value: 18}", adults.Args[0].Literal)
	}
}

// TestParseSegmentCallRejectsFieldAccessArgument confirms the grammar
// keeps call arguments stream-independent: an argument is a column name
// or a literal, never a field access.
func TestParseSegmentCallRejectsFieldAccessArgument(t *testing.T) {
	_, err := Parse(`pipeline main { in |> scrub(.email) |> out }`)
	if err == nil {
		t.Fatal("Parse succeeded, want an error for .email as a segment call argument")
	}
	if want := "not a field access"; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to contain %q", err, want)
	}
}

// TestParseBareIdentifierIsParamRef confirms the grammar relaxation
// behind scalar parameters: a bare identifier in expression position is
// not a parse error. It parses as an ast.ParamRef, and whether it
// resolves to anything is left to the checker.
func TestParseBareIdentifierIsParamRef(t *testing.T) {
	prog, err := Parse(`pipeline main { in |> filter(row) |> out }`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	filter := prog.Pipelines[0].Body[1].(*ast.Filter)
	ref, ok := filter.Pred.(*ast.ParamRef)
	if !ok || ref.Name != "row" {
		t.Errorf("Pred = %#v, want *ast.ParamRef{Name: row}", filter.Pred)
	}
}
