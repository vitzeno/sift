package parser

import (
	"strings"
	"testing"

	"github.com/vitzeno/sift/internal/ast"
)

func TestParseDeclassifyStages(t *testing.T) {
	tests := []struct {
		src string
		fn  string
	}{
		{`in |> mask(email) |> out`, "mask"},
		{`in |> hash(email) |> out`, "hash"},
		{`in |> redact(ssn, notes) |> out`, "redact"},
	}
	for _, tt := range tests {
		t.Run(tt.fn, func(t *testing.T) {
			prog, err := Parse(`pipeline main { ` + tt.src + ` }`)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			decl, ok := prog.Pipelines[0].Body[1].(*ast.Declassify)
			if !ok {
				t.Fatalf("body[1] = %#v, want *ast.Declassify", prog.Pipelines[0].Body[1])
			}
			if decl.Fn != tt.fn {
				t.Errorf("Fn = %q, want %q", decl.Fn, tt.fn)
			}
		})
	}
}

func TestParseRedactMultipleColumns(t *testing.T) {
	prog, err := Parse(`pipeline main { in |> redact(ssn, notes) |> out }`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	decl := prog.Pipelines[0].Body[1].(*ast.Declassify)
	if len(decl.Columns) != 2 || decl.Columns[0].Name != "ssn" || decl.Columns[1].Name != "notes" {
		t.Errorf("Columns = %+v, want [ssn, notes]", decl.Columns)
	}
}

func TestParseDeclassifyRejectsFieldAccess(t *testing.T) {
	_, err := Parse(`pipeline main { in |> mask(.email) |> out }`)
	if err == nil {
		t.Fatal("Parse succeeded, want an error for .email in mask's column list")
	}
	if !strings.Contains(err.Error(), "not a field access") {
		t.Errorf("error = %v, want the column-vs-field-access diagnostic", err)
	}
}
