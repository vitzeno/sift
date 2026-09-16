package parser

import (
	"strings"
	"testing"

	"github.com/vitzeno/sift/internal/ast"
)

func columnNames(cols []ast.ColumnRef) []string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return names
}

func TestParseSelect(t *testing.T) {
	prog, err := Parse(`pipeline main { in |> select(name, email) |> out }`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	sel, ok := prog.Pipelines[0].Body[1].(*ast.Select)
	if !ok {
		t.Fatalf("body[1] = %#v, want *ast.Select", prog.Pipelines[0].Body[1])
	}
	if got := columnNames(sel.Columns); len(got) != 2 || got[0] != "name" || got[1] != "email" {
		t.Errorf("Columns = %v, want [name, email]", got)
	}
}

func TestParseDrop(t *testing.T) {
	prog, err := Parse(`pipeline main { in |> drop(ssn, dob) |> out }`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	drop, ok := prog.Pipelines[0].Body[1].(*ast.Drop)
	if !ok {
		t.Fatalf("body[1] = %#v, want *ast.Drop", prog.Pipelines[0].Body[1])
	}
	if got := columnNames(drop.Columns); len(got) != 2 || got[0] != "ssn" || got[1] != "dob" {
		t.Errorf("Columns = %v, want [ssn, dob]", got)
	}
}

func TestParseSelectSingleColumn(t *testing.T) {
	prog, err := Parse(`pipeline main { in |> select(name) |> out }`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	sel := prog.Pipelines[0].Body[1].(*ast.Select)
	if len(sel.Columns) != 1 || sel.Columns[0].Name != "name" {
		t.Errorf("Columns = %+v, want [name]", sel.Columns)
	}
}

// TestParseColumnListRejectsFieldAccess locks the two-form convention:
// `.field` is never valid where select/drop expect a bare column name.
func TestParseColumnListRejectsFieldAccess(t *testing.T) {
	_, err := Parse(`pipeline main { in |> select(.name) |> out }`)
	if err == nil {
		t.Fatal("Parse succeeded, want an error for .name in select's column list")
	}
	want := `expected a column name "name", not a field access ".name"`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to contain %q", err, want)
	}
}

func TestParseSelectRequiresAtLeastOneColumn(t *testing.T) {
	_, err := Parse(`pipeline main { in |> select() |> out }`)
	if err == nil {
		t.Fatal("Parse succeeded, want an error for select() with no columns")
	}
}
