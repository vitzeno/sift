package checker

import (
	"strings"
	"testing"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
	"github.com/vitzeno/sift/internal/parser"
	"github.com/vitzeno/sift/internal/value"
)

func mustParse(t *testing.T, src string) *ast.Program {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	return prog
}

func mustCheck(t *testing.T, src string) *CheckedProgram {
	t.Helper()
	cp, err := Check(mustParse(t, src))
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	return cp
}

func checkErr(t *testing.T, src string) error {
	t.Helper()
	_, err := Check(mustParse(t, src))
	if err == nil {
		t.Fatalf("Check(%q) succeeded, want an error", src)
	}
	return err
}

// TestCheckAdultsFilter is design.md §7 Case A end to end: parse, check,
// and confirm the checked program is exactly what module 7 will need to
// build source |> filter |> sink from.
func TestCheckAdultsFilter(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("adults.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> out
}`
	cp := mustCheck(t, src)

	if cp.Source.Name != "in" || len(cp.Sinks) != 1 || cp.Sinks[0].Name != "out" {
		t.Errorf("Source/Sinks = %q/%v, want in/[out]", cp.Source.Name, cp.Sinks)
	}
	wantSchema := "{ name: string, age: int }"
	if got := cp.SourceSchema.String(); got != wantSchema {
		t.Errorf("SourceSchema = %s, want %s", got, wantSchema)
	}
	if got := cp.SinkSchema.String(); got != wantSchema {
		t.Errorf("SinkSchema = %s, want %s (filter doesn't change the schema)", got, wantSchema)
	}
	if len(cp.Stages) != 1 {
		t.Fatalf("Stages = %d, want 1", len(cp.Stages))
	}
	if _, ok := cp.Stages[0].(*ast.Filter); !ok {
		t.Errorf("Stages[0] = %#v, want *ast.Filter", cp.Stages[0])
	}
}

// TestCheckSourceSchemaOptional is OF1's checker-level slice of
// design/optional-fields.md: a `?` suffix on a schema field resolves into
// value.Type.Optional on that field only, leaving a plain field
// untouched. phone is dropped before the sink so this test stays about
// schema resolution, not the sink's discharge rule (OF2, tested
// separately); PII × optional coexistence (§4) is OF3, also separate.
func TestCheckSourceSchemaOptional(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, phone: string? })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> drop(phone) |> out
}`
	cp := mustCheck(t, src)

	wantSchema := "{ name: string, phone: string? }"
	if got := cp.SourceSchema.String(); got != wantSchema {
		t.Errorf("SourceSchema = %s, want %s", got, wantSchema)
	}
}

// TestCheckMultiSinkBroadcast is design-multisink.md MS-A/§5: a terminal
// list resolves to every named sink, in declared order, all sharing the
// one computed schema.
func TestCheckMultiSinkBroadcast(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")
sink out2 = jsonl("out2.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> out, out2
}`
	cp := mustCheck(t, src)
	if len(cp.Sinks) != 2 || cp.Sinks[0].Name != "out" || cp.Sinks[1].Name != "out2" {
		t.Fatalf("Sinks = %v, want [out, out2] in declared order", cp.Sinks)
	}
	wantSchema := "{ name: string, age: int }"
	if got := cp.SinkSchema.String(); got != wantSchema {
		t.Errorf("SinkSchema = %s, want %s (both sinks share it)", got, wantSchema)
	}
}

// TestCheckDuplicateSinkInBroadcastList is design-multisink.md MS-C: the
// same sink listed twice is a compile error, since it's always a literal
// double-write of the same row.
func TestCheckDuplicateSinkInBroadcastList(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> out, out
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `sink "out" listed twice`) {
		t.Errorf("error = %v, want a duplicate-sink error", err)
	}
}

// TestCheckPIIRejectsUnmaskedMultiSink is design-multisink.md MS-D: an
// unmasked @pii field reaching a broadcast list is a single compile
// error naming every sink it applies to, not one error per sink.
func TestCheckPIIRejectsUnmaskedMultiSink(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")
sink out2 = jsonl("out2.jsonl")

pipeline main {
  in |> out, out2
}`
	err := checkErr(t, src)
	want := `field "email" is @pii and reaches sink "out", "out2" unmasked; declassify with mask/hash/redact`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
}

// TestCheckPIIAllowsMaskedMultiSink is MS-D's success mode: masking once
// before the broadcast clears the tag for every listed sink.
func TestCheckPIIAllowsMaskedMultiSink(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")
sink out2 = jsonl("out2.jsonl")

pipeline main {
  in |> map({ ...row, email: mask(.email) }) |> out, out2
}`
	cp := mustCheck(t, src)
	if len(cp.Sinks) != 2 {
		t.Fatalf("Sinks = %d, want 2", len(cp.Sinks))
	}
}

// TestCheckPIIRejectsUnmaskedSink is design.md §7 Case B's failure mode:
// an unmasked @pii field reaching the sink must fail to compile with the
// message design.md §3 spells out almost verbatim.
func TestCheckPIIRejectsUnmaskedSink(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> out
}`
	err := checkErr(t, src)
	want := `field "email" is @pii and reaches sink "out" unmasked; declassify with mask/hash/redact`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
}

// TestCheckPIIAllowsMaskedSink is Case B's success mode: mask() clears
// the tag before the field reaches the sink.
func TestCheckPIIAllowsMaskedSink(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> map({ ...row, email: mask(.email) }) |> out
}`
	cp := mustCheck(t, src)
	f, ok := cp.SinkSchema.Lookup("email")
	if !ok {
		t.Fatal("email field missing from SinkSchema")
	}
	if f.Type.PII {
		t.Error("email field is still tagged PII after mask()")
	}
}

// TestCheckFieldNotInSchema matches CLAUDE.md's example diagnostic
// almost exactly: a typo'd field name reported against the real schema.
func TestCheckFieldNotInSchema(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> filter(.emial >= 18) |> out
}`
	err := checkErr(t, src)
	want := `field "emial" not in schema { name: string, age: int }`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
}

func TestCheckMapRecordSpreadOverridesInPlace(t *testing.T) {
	c := &checker{}
	input := value.Schema{Fields: []value.Field{
		{Name: "name", Type: value.Type{Kind: value.String}},
		{Name: "age", Type: value.Type{Kind: value.Int}},
	}}
	rec := &ast.RecordExpr{
		Spread: "row",
		Fields: []ast.RecordField{
			{Name: "age", Value: &ast.BinaryOp{
				Op:    lexer.GE,
				Left:  &ast.FieldAccess{Field: "age"},
				Right: &ast.IntLit{Value: 18},
			}},
			{Name: "note", Value: &ast.StringLit{Value: "adult check"}},
		},
	}

	out, err := c.checkMapRecord(rec, input)
	if err != nil {
		t.Fatalf("checkMapRecord error: %v", err)
	}
	if len(out.Fields) != 3 {
		t.Fatalf("Fields = %d, want 3: %+v", len(out.Fields), out.Fields)
	}
	if out.Fields[0].Name != "name" || out.Fields[0].Type.Kind != value.String {
		t.Errorf("Fields[0] = %+v, want untouched name:string", out.Fields[0])
	}
	if out.Fields[1].Name != "age" || out.Fields[1].Type.Kind != value.Bool {
		t.Errorf("Fields[1] = %+v, want age overridden in place to bool", out.Fields[1])
	}
	if out.Fields[2].Name != "note" || out.Fields[2].Type.Kind != value.String {
		t.Errorf("Fields[2] = %+v, want note:string appended at the end", out.Fields[2])
	}
}

func TestCheckMapRecordNoSpreadOnlyExplicitFields(t *testing.T) {
	c := &checker{}
	input := value.Schema{Fields: []value.Field{
		{Name: "name", Type: value.Type{Kind: value.String}},
		{Name: "age", Type: value.Type{Kind: value.Int}},
	}}
	rec := &ast.RecordExpr{Fields: []ast.RecordField{
		{Name: "adult", Value: &ast.BinaryOp{
			Op:    lexer.GE,
			Left:  &ast.FieldAccess{Field: "age"},
			Right: &ast.IntLit{Value: 18},
		}},
	}}

	out, err := c.checkMapRecord(rec, input)
	if err != nil {
		t.Fatalf("checkMapRecord error: %v", err)
	}
	if len(out.Fields) != 1 || out.Fields[0].Name != "adult" || out.Fields[0].Type.Kind != value.Bool {
		t.Errorf("Fields = %+v, want exactly one field adult:bool", out.Fields)
	}
}

func TestCheckMapRecordUnknownSpread(t *testing.T) {
	c := &checker{}
	rec := &ast.RecordExpr{Spread: "rows"}
	_, err := c.checkMapRecord(rec, value.Schema{})
	if err == nil || !strings.Contains(err.Error(), `"rows"`) {
		t.Errorf("error = %v, want it to mention the bad spread name", err)
	}
}

// TestCheckNamedSegmentInlining covers design.md §2's reusable-segment
// syntax: `clean`'s two stages must appear directly in the checked
// program's Stages, with no NameRef indirection left over.
func TestCheckNamedSegmentInlining(t *testing.T) {
	const src = `pipeline clean =
     check(.email != "", "missing email")
  |> map({ ...row, email: lower(trim(.email)) })

source in = csv("people.csv", schema: { name: string, email: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> clean |> out
}`
	cp := mustCheck(t, src)
	if len(cp.Stages) != 2 {
		t.Fatalf("Stages = %d, want 2 (clean's Check and Map, inlined)", len(cp.Stages))
	}
	if _, ok := cp.Stages[0].(*ast.Check); !ok {
		t.Errorf("Stages[0] = %#v, want *ast.Check", cp.Stages[0])
	}
	if _, ok := cp.Stages[1].(*ast.Map); !ok {
		t.Errorf("Stages[1] = %#v, want *ast.Map", cp.Stages[1])
	}
}

func TestCheckCycleDetectionSelfReference(t *testing.T) {
	const src = `pipeline loopy = filter(.x) |> loopy

source in = csv("x.csv", schema: { x: bool })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> loopy |> out
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `pipeline "loopy" is defined in terms of itself`) {
		t.Errorf("error = %v, want a self-reference cycle error", err)
	}
}

func TestCheckCycleDetectionMutualRecursion(t *testing.T) {
	const src = `pipeline a = filter(.x) |> b
pipeline b = filter(.x) |> a

source in = csv("x.csv", schema: { x: bool })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> a |> out
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), "is defined in terms of itself") {
		t.Errorf("error = %v, want a cycle error", err)
	}
}

func TestCheckSourceOrSinkReferencedMidChain(t *testing.T) {
	const src = `pipeline bad = in

source in = csv("x.csv", schema: { x: bool })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> bad |> out
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `source "in" cannot be referenced here`) {
		t.Errorf("error = %v, want a source-referenced-mid-chain error", err)
	}
}

func TestCheckRunnablePipelineCount(t *testing.T) {
	t.Run("zero", func(t *testing.T) {
		const src = `source in = csv("x.csv", schema: { x: bool })
sink out = jsonl("out.jsonl")

pipeline notRunnable = filter(.x)`
		err := checkErr(t, src)
		if !strings.Contains(err.Error(), "no runnable pipeline found") {
			t.Errorf("error = %v, want a no-runnable-pipeline error", err)
		}
	})

	t.Run("two", func(t *testing.T) {
		const src = `source in = csv("x.csv", schema: { x: bool })
sink out = jsonl("out.jsonl")

pipeline main1 { in |> out }
pipeline main2 { in |> out }`
		err := checkErr(t, src)
		if !strings.Contains(err.Error(), "found more than one runnable pipeline") {
			t.Errorf("error = %v, want a too-many-runnable-pipelines error", err)
		}
	})
}

func TestCheckDuplicateName(t *testing.T) {
	const src = `source in = csv("x.csv", schema: { x: bool })
sink in = jsonl("out.jsonl")`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `"in" is already declared`) {
		t.Errorf("error = %v, want a duplicate-name error", err)
	}
}

func TestCheckSourceSchemaErrors(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantSub string
	}{
		{
			"unknown type",
			`source in = csv("x.csv", schema: { name: strng })`,
			`unknown type "strng"`,
		},
		{
			"duplicate field",
			`source in = csv("x.csv", schema: { name: string, name: int })`,
			`duplicate field "name"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Check(mustParse(t, tt.src))
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %v, want it to contain %q", err, tt.wantSub)
			}
		})
	}
}

func checkExprErr(t *testing.T, src string, schema value.Schema) error {
	t.Helper()
	expr, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("ParseExpr(%q) error: %v", src, err)
	}
	c := &checker{}
	_, err = c.checkExpr(expr, schema)
	return err
}

func TestCheckBinaryOpTypeErrors(t *testing.T) {
	schema := value.Schema{Fields: []value.Field{
		{Name: "age", Type: value.Type{Kind: value.Int}},
		{Name: "name", Type: value.Type{Kind: value.String}},
		{Name: "flag", Type: value.Type{Kind: value.Bool}},
	}}
	tests := []struct {
		src     string
		wantSub string
	}{
		{".age + .name", "cannot apply + to int and string"},
		{".age - .name", "cannot apply - to int and string"},
		{".flag && .age", "&& requires bool operands"},
		{".age == .name", "cannot compare int and string"},
		{".age < .name", "cannot compare int and string"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			err := checkExprErr(t, tt.src, schema)
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %v, want it to contain %q", err, tt.wantSub)
			}
		})
	}
}

func TestCheckFunctionErrors(t *testing.T) {
	schema := value.Schema{Fields: []value.Field{
		{Name: "age", Type: value.Type{Kind: value.Int}},
		{Name: "email", Type: value.Type{Kind: value.String, PII: true}},
	}}
	tests := []struct {
		name    string
		src     string
		wantSub string
	}{
		{"unknown function", "foo(.email)", `unknown function "foo"`},
		{"too few args", "mask()", "mask() takes 1 argument, got 0"},
		{"too many args", "mask(.email, .email)", "mask() takes 1 argument, got 2"},
		{"wrong arg type", "mask(.age)", "mask() argument: expected string, got int"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkExprErr(t, tt.src, schema)
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %v, want it to contain %q", err, tt.wantSub)
			}
		})
	}
}

func TestCheckDeclassifyClearsPII(t *testing.T) {
	schema := value.Schema{Fields: []value.Field{
		{Name: "email", Type: value.Type{Kind: value.String, PII: true}},
	}}
	expr, err := parser.ParseExpr("mask(.email)")
	if err != nil {
		t.Fatalf("ParseExpr error: %v", err)
	}
	c := &checker{}
	got, err := c.checkExpr(expr, schema)
	if err != nil {
		t.Fatalf("checkExpr error: %v", err)
	}
	if got.PII {
		t.Error("mask(.email) should not be PII")
	}
}

func TestCheckNonDeclassifyPreservesPII(t *testing.T) {
	schema := value.Schema{Fields: []value.Field{
		{Name: "email", Type: value.Type{Kind: value.String, PII: true}},
	}}
	expr, err := parser.ParseExpr("upper(.email)")
	if err != nil {
		t.Fatalf("ParseExpr error: %v", err)
	}
	c := &checker{}
	got, err := c.checkExpr(expr, schema)
	if err != nil {
		t.Fatalf("checkExpr error: %v", err)
	}
	if !got.PII {
		t.Error("upper(.email) should still be PII -- it transforms the value, doesn't launder it")
	}
}
