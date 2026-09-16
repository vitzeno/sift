// CLI-level coverage for parameterized segments. Each scenario already
// has unit-level coverage in internal/parser and internal/checker; these
// confirm the same scenarios hold through the real CLI end to end
// (runFile: parse, check, build, run).
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScalarParamReusedWithDifferentLiterals: one
// `adults(min: int)` definition, called with different literals in two
// separate programs, each filtering correctly.
func TestScalarParamReusedWithDifferentLiterals(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\nTom,15\n")

	prog18 := filepath.Join(dir, "adults18.sift")
	out18 := filepath.Join(dir, "out18.jsonl")
	writeFile(t, prog18, `pipeline adults(min: int) = filter(.age >= min)

source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out18.jsonl")

pipeline main {
  in |> adults(18) |> out
}
`)
	if err := runFile(prog18, false); err != nil {
		t.Fatalf("runFile error: %v", err)
	}
	got18, err := os.ReadFile(out18)
	if err != nil {
		t.Fatalf("reading out18: %v", err)
	}
	if want := `{"name":"Ada","age":42}` + "\n"; string(got18) != want {
		t.Errorf("adults(18) output = %q, want %q", got18, want)
	}

	prog0 := filepath.Join(dir, "adults0.sift")
	out0 := filepath.Join(dir, "out0.jsonl")
	writeFile(t, prog0, `pipeline adults(min: int) = filter(.age >= min)

source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out0.jsonl")

pipeline main {
  in |> adults(0) |> out
}
`)
	if err := runFile(prog0, false); err != nil {
		t.Fatalf("runFile error: %v", err)
	}
	got0, err := os.ReadFile(out0)
	if err != nil {
		t.Fatalf("reading out0: %v", err)
	}
	want0 := `{"name":"Ada","age":42}
{"name":"Tom","age":15}
`
	if string(got0) != want0 {
		t.Errorf("adults(0) output = %q, want %q (both rows)", got0, want0)
	}
}

// TestColumnParamDeclassifiesTwoDifferentPIIColumns: one
// `scrub(col)` definition applied to two different @pii columns. Both
// get declassified and reach the sink cleanly.
func TestColumnParamDeclassifiesTwoDifferentPIIColumns(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,email,backup_email\nAda,ada@x.co,ada2@x.co\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	writeFile(t, siftPath, `pipeline scrub(col) = map({ ...row, col: mask(.col) })

source in = csv("people.csv", schema: { name: string, email: string @pii, backup_email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> scrub(email) |> scrub(backup_email) |> out
}
`)
	if err := runFile(siftPath, false); err != nil {
		t.Fatalf("runFile error: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if want := `{"name":"Ada","email":"********","backup_email":"*********"}` + "\n"; string(got) != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestNonDeclassifyingSegmentStillRejectedUnmasked: a segment
// that transforms but doesn't declassify keeps @pii on its argument
// column, so the field still can't reach a sink unmasked.
func TestNonDeclassifyingSegmentStillRejectedUnmasked(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,email\nAda,ada@x.co\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `pipeline touch(col) = map({ ...row, col: upper(.col) })

source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> touch(email) |> out
}
`)
	err := runFile(siftPath, false)
	if err == nil {
		t.Fatal("runFile succeeded, want the unmasked @pii field rejected")
	}
	if !strings.Contains(err.Error(), `field "email" is @pii and reaches sink`) {
		t.Errorf("error = %v, want the unmasked-PII sink rejection", err)
	}
}

// TestDeclassifyingSegmentOnNonPIIColumnRejected: applying a
// declassifying segment to a non-PII column is a compile error, with
// dual-site context naming the segment, its binding, and the call site.
func TestDeclassifyingSegmentOnNonPIIColumnRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `pipeline scrub(col) = mask(col)

source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> scrub(age) |> out
}
`)
	err := runFile(siftPath, false)
	if err == nil {
		t.Fatal("runFile succeeded, want scrub(age) rejected — age is not @pii")
	}
	msg := err.Error()
	if !strings.Contains(msg, `mask on non-PII column "age"`) {
		t.Errorf("error = %v, want the declassifier's own non-PII rejection", msg)
	}
	if !strings.Contains(msg, `in segment scrub(col = age)`) || !strings.Contains(msg, "instantiated at main:") {
		t.Errorf("error = %v, want dual-site context", msg)
	}
}

// TestBadColumnArgumentDualSiteDiagnostic: a misspelled column
// argument errors against the real schema, naming the segment, its
// binding, and the call site, not just a bare "field not in schema"
// pointing at synthesized AST.
func TestBadColumnArgumentDualSiteDiagnostic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `pipeline scrub(col) = map({ ...row, col: mask(.col) })

source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> scrub(emial) |> out
}
`)
	err := runFile(siftPath, false)
	if err == nil {
		t.Fatal("runFile succeeded, want the misspelled column argument rejected")
	}
	msg := err.Error()
	for _, want := range []string{
		`field "emial" not in schema`,
		`in segment scrub(col = emial)`,
		"instantiated at main:",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error = %v, want it to contain %q", msg, want)
		}
	}
}

// TestArityMismatchRejected: a segment call with the wrong
// number of arguments is a compile error with position.
func TestArityMismatchRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `pipeline adults(min: int) = filter(.age >= min)

source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> adults(18, 21) |> out
}
`)
	err := runFile(siftPath, false)
	if err == nil {
		t.Fatal("runFile succeeded, want the arity mismatch rejected")
	}
	if !strings.Contains(err.Error(), `takes 1 argument(s), got 2`) {
		t.Errorf("error = %v, want an arity-mismatch error", err)
	}
}

// TestSelfReferencingSegmentCallRejected: a parameterized
// segment defined in terms of itself is a compile error. Cycle
// detection holds through substitution.
func TestSelfReferencingSegmentCallRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,email\nAda,ada@x.co\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `pipeline loopy(col) = mask(col) |> loopy(col)

source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> loopy(email) |> out
}
`)
	err := runFile(siftPath, false)
	if err == nil {
		t.Fatal("runFile succeeded, want the self-referencing segment rejected")
	}
	if !strings.Contains(err.Error(), `pipeline "loopy" is defined in terms of itself`) {
		t.Errorf("error = %v, want a cycle error", err)
	}
}
