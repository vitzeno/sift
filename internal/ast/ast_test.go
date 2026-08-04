package ast

import (
	"testing"

	"github.com/vitzeno/sift/internal/lexer"
)

// Compile-time checks that every concrete node satisfies its marker
// interface. These catch an accidental typo (e.g. a method on the
// wrong receiver type) at build time instead of silently producing a
// type that can never go in an Expr/Stage slice.
var (
	_ Expr = (*FieldAccess)(nil)
	_ Expr = (*IntLit)(nil)
	_ Expr = (*DoubleLit)(nil)
	_ Expr = (*StringLit)(nil)
	_ Expr = (*BoolLit)(nil)
	_ Expr = (*BinaryOp)(nil)
	_ Expr = (*Call)(nil)
	_ Expr = (*RecordExpr)(nil)

	_ Stage = (*NameRef)(nil)
	_ Stage = (*Filter)(nil)
	_ Stage = (*Map)(nil)
	_ Stage = (*Check)(nil)
	_ Stage = (*Select)(nil)
	_ Stage = (*Drop)(nil)
	_ Stage = (*Rename)(nil)
	_ Stage = (*Limit)(nil)
	_ Stage = (*Offset)(nil)
	_ Stage = (*Declassify)(nil)
)

// TestAdultsFilterShape hand-builds design.md §7 Case A's adults.sift as
// a tree, the way module 5's parser will eventually produce it. It's
// less "test the AST does something" (it doesn't; these are inert
// structs) and more proof that the node types actually compose into the
// shape the language needs, with positions intact for diagnostics.
func TestAdultsFilterShape(t *testing.T) {
	prog := &Program{
		Sources: []*SourceDecl{{
			Name:   "in",
			Format: "csv",
			Path:   "people.csv",
			Schema: SchemaLit{Fields: []SchemaField{
				{Name: "name", TypeName: "string", Pos: lexer.Pos{Line: 1, Col: 30}},
				{Name: "age", TypeName: "int", Pos: lexer.Pos{Line: 1, Col: 45}},
			}},
			Pos: lexer.Pos{Line: 1, Col: 1},
		}},
		Sinks: []*SinkDecl{{
			Name:   "out",
			Format: "jsonl",
			Path:   "adults.jsonl",
			Pos:    lexer.Pos{Line: 2, Col: 1},
		}},
		Pipelines: []*PipelineDecl{{
			Name: "main",
			Body: []Stage{
				&NameRef{Name: "in", Pos: lexer.Pos{Line: 4, Col: 3}},
				&Filter{
					Pred: &BinaryOp{
						Op:    lexer.GE,
						Left:  &FieldAccess{Field: "age", Pos: lexer.Pos{Line: 4, Col: 15}},
						Right: &IntLit{Value: 18, Pos: lexer.Pos{Line: 4, Col: 23}},
						Pos:   lexer.Pos{Line: 4, Col: 15},
					},
					Pos: lexer.Pos{Line: 4, Col: 8},
				},
				&NameRef{Name: "out", Pos: lexer.Pos{Line: 4, Col: 30}},
			},
			Pos: lexer.Pos{Line: 3, Col: 1},
		}},
	}

	if len(prog.Sources) != 1 || prog.Sources[0].Name != "in" {
		t.Fatalf("Sources = %+v, want one source named \"in\"", prog.Sources)
	}
	if got, want := prog.Sources[0].Schema.Fields[1].TypeName, "int"; got != want {
		t.Errorf("age field TypeName = %q, want %q", got, want)
	}

	body := prog.Pipelines[0].Body
	if len(body) != 3 {
		t.Fatalf("pipeline body has %d stages, want 3", len(body))
	}
	if ref, ok := body[0].(*NameRef); !ok || ref.Name != "in" {
		t.Errorf("body[0] = %#v, want NameRef{Name: \"in\"}", body[0])
	}
	filter, ok := body[1].(*Filter)
	if !ok {
		t.Fatalf("body[1] = %#v, want *Filter", body[1])
	}
	cond, ok := filter.Pred.(*BinaryOp)
	if !ok || cond.Op != lexer.GE {
		t.Fatalf("filter.Pred = %#v, want BinaryOp{Op: GE}", filter.Pred)
	}
	if fa, ok := cond.Left.(*FieldAccess); !ok || fa.Field != "age" {
		t.Errorf("cond.Left = %#v, want FieldAccess{Field: \"age\"}", cond.Left)
	}
	if lit, ok := cond.Right.(*IntLit); !ok || lit.Value != 18 {
		t.Errorf("cond.Right = %#v, want IntLit{Value: 18}", cond.Right)
	}
	if ref, ok := body[2].(*NameRef); !ok || ref.Name != "out" {
		t.Errorf("body[2] = %#v, want NameRef{Name: \"out\"}", body[2])
	}
}

// TestPIIDeclassifyShape covers design.md §7 Case B's shape: a @pii
// schema field, and a map stage that declassifies it with mask() before
// it can reach a sink.
func TestPIIDeclassifyShape(t *testing.T) {
	schema := SchemaLit{Fields: []SchemaField{
		{Name: "email", TypeName: "string", PII: true},
	}}
	if !schema.Fields[0].PII {
		t.Fatal("email field should be tagged PII")
	}

	m := &Map{
		Record: &RecordExpr{
			Spread: "row",
			Fields: []RecordField{
				{Name: "email", Value: &Call{
					Fn:   "mask",
					Args: []Expr{&FieldAccess{Field: "email"}},
				}},
			},
		},
	}

	if m.Record.Spread != "row" {
		t.Errorf("Spread = %q, want \"row\"", m.Record.Spread)
	}
	call, ok := m.Record.Fields[0].Value.(*Call)
	if !ok || call.Fn != "mask" {
		t.Fatalf("email field value = %#v, want Call{Fn: \"mask\"}", m.Record.Fields[0].Value)
	}
	if len(call.Args) != 1 {
		t.Fatalf("mask() called with %d args, want 1", len(call.Args))
	}
	if fa, ok := call.Args[0].(*FieldAccess); !ok || fa.Field != "email" {
		t.Errorf("mask() arg = %#v, want FieldAccess{Field: \"email\"}", call.Args[0])
	}
}

// TestCheckStageShape covers `check(<bool expr>, "<reason>")`, v0's
// third built-in stage, which neither acceptance case exercises but
// CLAUDE.md's Definition of Done requires as a v0 stage.
func TestCheckStageShape(t *testing.T) {
	c := &Check{
		Cond: &BinaryOp{
			Op:    lexer.NE,
			Left:  &FieldAccess{Field: "email"},
			Right: &StringLit{Value: ""},
		},
		Reason: "missing email",
	}

	if c.Reason != "missing email" {
		t.Errorf("Reason = %q, want %q", c.Reason, "missing email")
	}
	cond, ok := c.Cond.(*BinaryOp)
	if !ok || cond.Op != lexer.NE {
		t.Fatalf("Cond = %#v, want BinaryOp{Op: NE}", c.Cond)
	}
}
