package parser

import (
	"strings"
	"testing"

	"github.com/vitzeno/sift/internal/ast"
)

func TestParseRenameSinglePair(t *testing.T) {
	prog, err := Parse(`pipeline main { in |> rename(email_addr: email) |> out }`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	ren, ok := prog.Pipelines[0].Body[1].(*ast.Rename)
	if !ok {
		t.Fatalf("body[1] = %#v, want *ast.Rename", prog.Pipelines[0].Body[1])
	}
	if len(ren.Pairs) != 1 || ren.Pairs[0].Old != "email_addr" || ren.Pairs[0].New != "email" {
		t.Errorf("Pairs = %+v, want [{email_addr, email}]", ren.Pairs)
	}
}

func TestParseRenameMultiplePairs(t *testing.T) {
	prog, err := Parse(`pipeline main { in |> rename(email_addr: email, dob: birth_date) |> out }`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	ren := prog.Pipelines[0].Body[1].(*ast.Rename)
	if len(ren.Pairs) != 2 {
		t.Fatalf("Pairs = %+v, want 2 entries", ren.Pairs)
	}
	if ren.Pairs[1].Old != "dob" || ren.Pairs[1].New != "birth_date" {
		t.Errorf("Pairs[1] = %+v, want {dob, birth_date}", ren.Pairs[1])
	}
}

func TestParseRenameRejectsFieldAccess(t *testing.T) {
	_, err := Parse(`pipeline main { in |> rename(.email: email) |> out }`)
	if err == nil {
		t.Fatal("Parse succeeded, want an error for .email in a rename pair")
	}
	if !strings.Contains(err.Error(), "not a field access") {
		t.Errorf("error = %v, want the column-vs-field-access diagnostic", err)
	}
}
