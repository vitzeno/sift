package parser

import (
	"strings"
	"testing"
)

// TestParseSourceOpts covers generic keyword arguments: a source
// declaration may carry keyword arguments beyond schema, in any
// order relative to it, each a bare literal collected into
// ast.SourceDecl.Opts untouched by the parser.
func TestParseSourceOpts(t *testing.T) {
	const src = `source in = xlsx("people.xlsx", sheet: "Q1", header_row: 3, schema: { name: string })`

	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(prog.Sources) != 1 {
		t.Fatalf("Sources = %d, want 1", len(prog.Sources))
	}
	s := prog.Sources[0]
	if len(s.Schema.Fields) != 1 || s.Schema.Fields[0].Name != "name" {
		t.Errorf("schema = %+v, want one field \"name\"", s.Schema)
	}
	if len(s.Opts) != 2 {
		t.Fatalf("Opts = %d, want 2", len(s.Opts))
	}
	if s.Opts[0].Name != "sheet" || exprString(s.Opts[0].Value) != `"Q1"` {
		t.Errorf("Opts[0] = %+v, want sheet: \"Q1\"", s.Opts[0])
	}
	if s.Opts[1].Name != "header_row" || exprString(s.Opts[1].Value) != "3" {
		t.Errorf("Opts[1] = %+v, want header_row: 3", s.Opts[1])
	}
}

// TestParseSourceOptsBeforeSchema confirms opts and schema can appear in
// either order. The grammar doesn't privilege schema's position, only
// its presence.
func TestParseSourceOptsBeforeSchema(t *testing.T) {
	const src = `source in = xlsx("people.xlsx", header_row: 3, schema: { name: string })`

	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	s := prog.Sources[0]
	if len(s.Opts) != 1 || s.Opts[0].Name != "header_row" {
		t.Errorf("Opts = %+v, want one header_row opt", s.Opts)
	}
	if len(s.Schema.Fields) != 1 {
		t.Errorf("schema = %+v, want one field", s.Schema)
	}
}

func TestParseSourceMissingSchema(t *testing.T) {
	const src = `source in = xlsx("people.xlsx", sheet: "Q1")`
	_, err := Parse(src)
	if err == nil {
		t.Fatal("Parse error = nil, want an error about the missing schema kwarg")
	}
	if got := err.Error(); !strings.Contains(got, `missing required "schema"`) {
		t.Errorf("error = %q, want it to mention the missing schema kwarg", got)
	}
}

func TestParseSourceDuplicateSchema(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string }, schema: { age: int })`
	_, err := Parse(src)
	if err == nil {
		t.Fatal("Parse error = nil, want a duplicate-schema error")
	}
	if got := err.Error(); !strings.Contains(got, `duplicate "schema"`) {
		t.Errorf("error = %q, want it to mention the duplicate schema kwarg", got)
	}
}

// TestParseSourceColumns covers the columns kwarg: a schema identifier
// mapped to the raw header string to match
// instead of its own name, collected into ast.SourceDecl.Columns.
func TestParseSourceColumns(t *testing.T) {
	const src = `source in = csv("transactions.csv",
  schema:  { txn_id: int, txn_date: string, amount: double },
  columns: { txn_id: "Transaction ID", txn_date: "Date" }
)`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	s := prog.Sources[0]
	if len(s.Columns) != 2 {
		t.Fatalf("Columns = %d, want 2", len(s.Columns))
	}
	if s.Columns[0].Field != "txn_id" || s.Columns[0].Header != "Transaction ID" {
		t.Errorf("Columns[0] = %+v, want {Field: txn_id, Header: \"Transaction ID\"}", s.Columns[0])
	}
	if s.Columns[1].Field != "txn_date" || s.Columns[1].Header != "Date" {
		t.Errorf("Columns[1] = %+v, want {Field: txn_date, Header: \"Date\"}", s.Columns[1])
	}
	// amount has no alias entry at all -- columns is fully optional per
	// field, not just per source.
	if len(s.Schema.Fields) != 3 {
		t.Errorf("Schema.Fields = %d, want 3", len(s.Schema.Fields))
	}
}

// TestParseSourceColumnsIsOptional confirms a source with no columns
// kwarg at all still parses fine, unaffected by this feature existing.
func TestParseSourceColumnsIsOptional(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string })`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if prog.Sources[0].Columns != nil {
		t.Errorf("Columns = %+v, want nil", prog.Sources[0].Columns)
	}
}

func TestParseSourceDuplicateColumns(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string }, columns: { name: "Name" }, columns: { name: "Full Name" })`
	_, err := Parse(src)
	if err == nil {
		t.Fatal("Parse error = nil, want a duplicate-columns error")
	}
	if got := err.Error(); !strings.Contains(got, `duplicate "columns"`) {
		t.Errorf("error = %q, want it to mention the duplicate columns kwarg", got)
	}
}
