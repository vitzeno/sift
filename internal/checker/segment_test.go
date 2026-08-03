package checker

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vitzeno/sift/internal/ast"
)

// TestCheckScalarParamReuseAcrossPrograms is design/segments.md PS-A: one
// `adults(min: int)` definition, instantiated with two different literals
// in two separate programs, each producing the correctly substituted
// filter predicate -- proof that monomorphization doesn't leak state
// between call sites.
func TestCheckScalarParamReuseAcrossPrograms(t *testing.T) {
	const tmpl = `pipeline adults(min: int) = filter(.age >= min)

source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> adults(%d) |> out
}`

	for _, min := range []int64{18, 21} {
		src := fmt.Sprintf(tmpl, min)
		cp := mustCheck(t, src)
		if len(cp.Stages) != 1 {
			t.Fatalf("Stages = %d, want 1 (the substituted filter)", len(cp.Stages))
		}
		filter, ok := cp.Stages[0].(*ast.Filter)
		if !ok {
			t.Fatalf("Stages[0] = %#v, want *ast.Filter", cp.Stages[0])
		}
		bin := filter.Pred.(*ast.BinaryOp)
		lit, ok := bin.Right.(*ast.IntLit)
		if !ok || lit.Value != min {
			t.Errorf("min=%d: Pred.Right = %#v, want *ast.IntLit{Value: %d}", min, bin.Right, min)
		}
	}
}

// TestCheckColumnParamReuseAcrossTwoCallSites is PS-B: one `scrub(col)`
// definition applied to two different @pii columns in the same pipeline;
// both are declassified and reach the sink cleanly.
func TestCheckColumnParamReuseAcrossTwoCallSites(t *testing.T) {
	const src = `pipeline scrub(col) = map({ ...row, col: mask(.col) })

source in = csv("people.csv", schema: { name: string, email: string @pii, backup_email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> scrub(email) |> scrub(backup_email) |> out
}`
	cp := mustCheck(t, src)
	for _, name := range []string{"email", "backup_email"} {
		f, ok := cp.SinkSchema.Lookup(name)
		if !ok {
			t.Fatalf("SinkSchema missing field %q", name)
		}
		if f.Type.PII {
			t.Errorf("field %q still @pii after scrub, want declassified", name)
		}
	}
}

// TestCheckSegmentCallComposesWithBuiltinStage is PS-C: a parameterized
// call composes in a `|>` chain interchangeably with a built-in stage.
func TestCheckSegmentCallComposesWithBuiltinStage(t *testing.T) {
	const src = `pipeline scrub(col) = map({ ...row, col: mask(.col) })

source in = csv("people.csv", schema: { name: string, age: int, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> scrub(email) |> filter(.age >= 18) |> out
}`
	cp := mustCheck(t, src)
	if len(cp.Stages) != 2 {
		t.Fatalf("Stages = %d, want 2 (scrub's Map, then the builtin Filter)", len(cp.Stages))
	}
	if _, ok := cp.Stages[0].(*ast.Map); !ok {
		t.Errorf("Stages[0] = %#v, want *ast.Map", cp.Stages[0])
	}
	if _, ok := cp.Stages[1].(*ast.Filter); !ok {
		t.Errorf("Stages[1] = %#v, want *ast.Filter", cp.Stages[1])
	}
}

// TestCheckNonDeclassifyingSegmentPreservesPII is PS-D: a segment that
// transforms but doesn't declassify keeps @pii on a @pii argument column
// -- it still can't reach a sink unmasked.
func TestCheckNonDeclassifyingSegmentPreservesPII(t *testing.T) {
	const src = `pipeline touch(col) = map({ ...row, col: upper(.col) })

source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> touch(email) |> out
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `field "email" is @pii and reaches sink`) {
		t.Errorf("error = %v, want the unmasked-PII sink rejection", err)
	}
}

// TestCheckDeclassifyingSegmentOnNonPIIColumnRejected is PS-E: applying a
// declassifying segment to a non-PII column is the same compile error
// the declassifier stage raises directly, with dual-site context.
//
// decision: scrub is defined with the *stage*-form declassifier
// (`mask(col)`), not the expression form (`mask(.col)`) -- only
// checkDeclassify (the stage) enforces "the target must already be
// @pii"; checkCall's expression-form mask has no such precondition (it
// just clears PII on whatever string it's given). Using the stage form
// is what makes this scenario an actual compile error to reuse, per §5's
// "no new PII rule here -- reuses the existing ones."
func TestCheckDeclassifyingSegmentOnNonPIIColumnRejected(t *testing.T) {
	const src = `pipeline scrub(col) = mask(col)

source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> scrub(age) |> out
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `mask on non-PII column "age"`) {
		t.Errorf("error = %v, want the declassifier's own non-PII rejection", err)
	}
	if !strings.Contains(err.Error(), `in segment scrub(col = age)`) {
		t.Errorf("error = %v, want dual-site context naming the segment and binding", err)
	}
	if !strings.Contains(err.Error(), `instantiated at main:7`) {
		t.Errorf("error = %v, want dual-site context naming the call site", err)
	}
}

// TestCheckBadColumnArgumentDualSiteDiagnostic is PS-F, and design/segments.md
// §4's own worked example: a misspelled column argument errors against
// the real schema, naming the segment, its bindings, and the call site --
// not just "field not in schema" pointing at synthesized AST.
func TestCheckBadColumnArgumentDualSiteDiagnostic(t *testing.T) {
	const src = `pipeline scrub(col) = map({ ...row, col: mask(.col) })

source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> scrub(emial) |> out
}`
	err := checkErr(t, src)
	msg := err.Error()
	if !strings.Contains(msg, `field "emial" not in schema`) {
		t.Errorf("error = %v, want it to cite the missing field against the real schema", msg)
	}
	if !strings.Contains(msg, `in segment scrub(col = emial)`) {
		t.Errorf("error = %v, want the segment name and binding", msg)
	}
	if !strings.Contains(msg, `instantiated at main:7`) {
		t.Errorf("error = %v, want the call-site pipeline and line", msg)
	}
}

// TestCheckSegmentArityMismatch and TestCheckSegmentArgumentTypeMismatch
// are PS-G: wrong argument count, or a literal of the wrong type, is a
// compile error with position.
func TestCheckSegmentArityMismatch(t *testing.T) {
	const src = `pipeline adults(min: int) = filter(.age >= min)

source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> adults(18, 21) |> out
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `takes 1 argument(s), got 2`) {
		t.Errorf("error = %v, want an arity-mismatch error", err)
	}
}

func TestCheckSegmentArgumentTypeMismatch(t *testing.T) {
	const src = `pipeline adults(min: int) = filter(.age >= min)

source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> adults("eighteen") |> out
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `expects int, got string`) {
		t.Errorf("error = %v, want a type-mismatch error", err)
	}
}

// TestCheckSegmentColumnArgumentRejectsLiteral confirms the reverse
// mismatch: a column parameter fed a literal, not a column name.
func TestCheckSegmentColumnArgumentRejectsLiteral(t *testing.T) {
	const src = `pipeline scrub(col) = map({ ...row, col: mask(.col) })

source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> scrub(18) |> out
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `expects a column name, got a literal`) {
		t.Errorf("error = %v, want a column/literal mismatch error", err)
	}
}

// TestCheckSegmentCycleDetectionThroughCall is PS-H: a parameterized
// segment that calls itself, directly, is a compile error -- the
// existing cycle detection still applies through substitution.
func TestCheckSegmentCycleDetectionThroughCall(t *testing.T) {
	const src = `pipeline loopy(col) = mask(col) |> loopy(col)

source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> loopy(email) |> out
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `pipeline "loopy" is defined in terms of itself`) {
		t.Errorf("error = %v, want a self-reference cycle error", err)
	}
}

// TestCheckDuplicateParamNameRejected and
// TestCheckUnknownScalarParamTypeRejected confirm a segment's parameter
// list is validated once at declaration time, regardless of whether any
// call site ever instantiates it.
func TestCheckDuplicateParamNameRejected(t *testing.T) {
	const src = `pipeline bad(col, col: int) = filter(.x)

source in = csv("x.csv", schema: { x: bool })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> out
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `duplicate parameter "col"`) {
		t.Errorf("error = %v, want a duplicate-parameter error", err)
	}
}

func TestCheckUnknownScalarParamTypeRejected(t *testing.T) {
	const src = `pipeline bad(min: number) = filter(.x)

source in = csv("x.csv", schema: { x: bool })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> out
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `unknown type "number"`) {
		t.Errorf("error = %v, want an unknown-type error", err)
	}
}

// TestCheckBareIdentifierOutsideSegmentIsUndefined confirms a bare
// identifier used where no enclosing segment declares it as a scalar
// parameter is rejected as an undefined name -- the same diagnostic an
// unresolvable stage NameRef gets.
func TestCheckBareIdentifierOutsideSegmentIsUndefined(t *testing.T) {
	const src = `source in = csv("x.csv", schema: { x: bool })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> filter(row) |> out
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `undefined name "row"`) {
		t.Errorf("error = %v, want an undefined-name error", err)
	}
}
