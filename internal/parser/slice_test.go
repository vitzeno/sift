package parser

import (
	"strings"
	"testing"

	"github.com/vitzeno/sift/internal/ast"
)

func TestParseLimit(t *testing.T) {
	prog, err := Parse(`pipeline main { in |> limit(1000) |> out }`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	lim, ok := prog.Pipelines[0].Body[1].(*ast.Limit)
	if !ok {
		t.Fatalf("body[1] = %#v, want *ast.Limit", prog.Pipelines[0].Body[1])
	}
	if lim.N != 1000 {
		t.Errorf("N = %d, want 1000", lim.N)
	}
}

func TestParseOffset(t *testing.T) {
	prog, err := Parse(`pipeline main { in |> offset(50) |> out }`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	off, ok := prog.Pipelines[0].Body[1].(*ast.Offset)
	if !ok {
		t.Fatalf("body[1] = %#v, want *ast.Offset", prog.Pipelines[0].Body[1])
	}
	if off.N != 50 {
		t.Errorf("N = %d, want 50", off.N)
	}
}

func TestParseLimitRejectsNonLiteralArg(t *testing.T) {
	_, err := Parse(`pipeline main { in |> limit(.n) |> out }`)
	if err == nil {
		t.Fatal("Parse succeeded, want an error for a non-literal limit argument")
	}
	if !strings.Contains(err.Error(), "expected INT") {
		t.Errorf("error = %v, want it to say an INT token was expected", err)
	}
}

// TestParseTakeRejectedWithHint and TestParseStageSkipRejectedWithHint
// lock design-improvements.md §3: "take"/"skip" are rejected by name,
// with a specific hint toward the real names, not just a generic
// unknown-stage error.
func TestParseTakeRejectedWithHint(t *testing.T) {
	_, err := Parse(`pipeline main { in |> take(10) |> out }`)
	if err == nil {
		t.Fatal("Parse succeeded, want \"take\" rejected")
	}
	if !strings.Contains(err.Error(), `did you mean "limit"`) {
		t.Errorf("error = %v, want it to suggest \"limit\"", err)
	}
}

func TestParseStageSkipRejectedWithHint(t *testing.T) {
	_, err := Parse(`pipeline main { in |> skip(10) |> out }`)
	if err == nil {
		t.Fatal("Parse succeeded, want \"skip\" rejected as a stage name")
	}
	if !strings.Contains(err.Error(), "reserved for the error policy") || !strings.Contains(err.Error(), `"offset"`) {
		t.Errorf("error = %v, want it to explain the collision and suggest \"offset\"", err)
	}
}

func TestParseUnknownStageMessageListsLimitAndOffset(t *testing.T) {
	_, err := Parse(`pipeline main { in |> bogus(x) |> out }`)
	if err == nil {
		t.Fatal("Parse succeeded, want an unknown-stage error")
	}
	if !strings.Contains(err.Error(), "limit") || !strings.Contains(err.Error(), "offset") {
		t.Errorf("error = %v, want it to list limit and offset among the built-ins", err)
	}
}
