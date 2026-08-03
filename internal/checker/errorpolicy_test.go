package checker

import (
	"strings"
	"testing"

	"github.com/vitzeno/sift/internal/ast"
)

const errPolicyBaseSrc = `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("adults.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> out
}`

// TestCheckErrorPolicyDefaultsToAbort confirms design.md §2's default
// holds when a program declares no `on error` at all — the regression
// guard design-errors.md §7 asks for: every v0 program (none of which
// ever wrote `on error`) must keep behaving exactly as before.
func TestCheckErrorPolicyDefaultsToAbort(t *testing.T) {
	cp := mustCheck(t, errPolicyBaseSrc)
	if cp.ErrorPolicy != ast.ErrorAbort {
		t.Errorf("ErrorPolicy = %v, want ErrorAbort", cp.ErrorPolicy)
	}
	if cp.ErrorSink != nil {
		t.Errorf("ErrorSink = %+v, want nil", cp.ErrorSink)
	}
}

func TestCheckErrorPolicyExplicitAbort(t *testing.T) {
	cp := mustCheck(t, "on error abort\n"+errPolicyBaseSrc)
	if cp.ErrorPolicy != ast.ErrorAbort {
		t.Errorf("ErrorPolicy = %v, want ErrorAbort", cp.ErrorPolicy)
	}
}

func TestCheckErrorPolicySkip(t *testing.T) {
	cp := mustCheck(t, "on error skip\n"+errPolicyBaseSrc)
	if cp.ErrorPolicy != ast.ErrorSkip {
		t.Errorf("ErrorPolicy = %v, want ErrorSkip", cp.ErrorPolicy)
	}
	if cp.ErrorSink != nil {
		t.Errorf("ErrorSink = %+v, want nil for skip", cp.ErrorSink)
	}
}

// TestCheckErrorPolicyRouteResolvesSink confirms a route target
// resolves to the actual declared sink, with no schema attached (the
// checker never threads a pipeline schema into it — design-errors.md
// §5).
func TestCheckErrorPolicyRouteResolvesSink(t *testing.T) {
	const src = `on error |> errs
source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("adults.jsonl")
sink errs = jsonl("errors.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> out
}`
	cp := mustCheck(t, src)
	if cp.ErrorPolicy != ast.ErrorRoute {
		t.Errorf("ErrorPolicy = %v, want ErrorRoute", cp.ErrorPolicy)
	}
	if cp.ErrorSink == nil {
		t.Fatal("ErrorSink is nil, want the resolved sink")
	}
	if cp.ErrorSink.Name != "errs" || cp.ErrorSink.Path != "errors.jsonl" {
		t.Errorf("ErrorSink = %+v, want {Name: errs, Path: errors.jsonl}", cp.ErrorSink)
	}
}

func TestCheckErrorPolicyRouteToUndefinedName(t *testing.T) {
	err := checkErr(t, "on error |> nope\n"+errPolicyBaseSrc)
	if !strings.Contains(err.Error(), `undefined name "nope"`) {
		t.Errorf("error = %v, want it to mention the undefined name", err)
	}
}

// TestCheckErrorPolicyRouteToNonSink confirms the route target must
// specifically be a sink — naming the source, or a named pipeline
// segment, is rejected with a clear reason, not just "undefined".
func TestCheckErrorPolicyRouteToNonSink(t *testing.T) {
	err := checkErr(t, "on error |> in\n"+errPolicyBaseSrc)
	if !strings.Contains(err.Error(), `"in" is not a sink`) {
		t.Errorf("error = %v, want it to say \"in\" is not a sink", err)
	}
}

// TestCheckErrorPolicyDoesNotAffectSinkSchema confirms routing has no
// effect whatsoever on the main pipeline's checked schema or PII
// enforcement — the two are entirely independent concerns.
func TestCheckErrorPolicyDoesNotAffectSinkSchema(t *testing.T) {
	const src = `on error |> errs
source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")
sink errs = jsonl("errors.jsonl")

pipeline main {
  in |> out
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), "is @pii and reaches sink") {
		t.Errorf("error = %v, want the usual PII rejection, unaffected by routing", err)
	}
}
