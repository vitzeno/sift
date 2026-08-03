package parser

import (
	"strings"
	"testing"

	"github.com/vitzeno/sift/internal/ast"
)

func TestParseErrorPolicyAbort(t *testing.T) {
	prog, err := Parse(`on error abort`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if prog.ErrorPolicy == nil {
		t.Fatal("ErrorPolicy is nil, want it set")
	}
	if prog.ErrorPolicy.Kind != ast.ErrorAbort {
		t.Errorf("Kind = %v, want ErrorAbort", prog.ErrorPolicy.Kind)
	}
	if prog.ErrorPolicy.Target != nil {
		t.Errorf("Target = %+v, want nil for abort", prog.ErrorPolicy.Target)
	}
}

func TestParseErrorPolicySkip(t *testing.T) {
	prog, err := Parse(`on error skip`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if prog.ErrorPolicy.Kind != ast.ErrorSkip {
		t.Errorf("Kind = %v, want ErrorSkip", prog.ErrorPolicy.Kind)
	}
}

func TestParseErrorPolicyRoute(t *testing.T) {
	prog, err := Parse(`on error |> errors`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if prog.ErrorPolicy.Kind != ast.ErrorRoute {
		t.Errorf("Kind = %v, want ErrorRoute", prog.ErrorPolicy.Kind)
	}
	if prog.ErrorPolicy.Target == nil || prog.ErrorPolicy.Target.Name != "errors" {
		t.Errorf("Target = %+v, want NameRef{errors}", prog.ErrorPolicy.Target)
	}
}

// TestParseErrorPolicyAmongOtherDecls confirms `on error` can appear
// alongside source/sink/pipeline declarations in any position, matching
// how those three are already allowed to interleave freely.
func TestParseErrorPolicyAmongOtherDecls(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, age: int })
on error skip
sink out = jsonl("adults.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> out
}`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if prog.ErrorPolicy == nil || prog.ErrorPolicy.Kind != ast.ErrorSkip {
		t.Errorf("ErrorPolicy = %+v, want ErrorSkip", prog.ErrorPolicy)
	}
	if len(prog.Sources) != 1 || len(prog.Sinks) != 1 || len(prog.Pipelines) != 1 {
		t.Errorf("Sources/Sinks/Pipelines = %d/%d/%d, want 1/1/1",
			len(prog.Sources), len(prog.Sinks), len(prog.Pipelines))
	}
}

func TestParseErrorPolicyDuplicateRejected(t *testing.T) {
	_, err := Parse("on error abort\non error skip")
	if err == nil {
		t.Fatal("Parse succeeded, want an error for a duplicate 'on error' declaration")
	}
	if !strings.Contains(err.Error(), "only one 'on error' declaration") {
		t.Errorf("error = %v, want it to mention the duplicate declaration", err)
	}
}

func TestParseErrorPolicyBadForm(t *testing.T) {
	_, err := Parse("on error maybe")
	if err == nil {
		t.Fatal("Parse succeeded, want an error for an unrecognized error-policy form")
	}
	if !strings.Contains(err.Error(), "expected 'abort', 'skip', or '|>'") {
		t.Errorf("error = %v, want it to name the valid forms", err)
	}
}
