// This file maps directly to design-errors.md §7's acceptance list.
// ERR-A/B/C already have thorough coverage in error_policy_test.go
// (abort/skip/route against a failing `check`); this file rounds out
// ERR-D (bad-cell coercion failure) under all three policies and ERR-E
// (a source-level infra-fatal error) through the real CLI, plus one
// stronger ERR-A assertion on the full diagnostic string.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestERR_A_AbortDiagnosticCarriesSourceOrdinalAndReason strengthens
// error_policy_test.go's TestRunOnErrorAbort: design-errors.md §7 asks
// specifically for "a diagnostic carrying source name, ordinal, and
// reason" — not just *some* error.
func TestERR_A_AbortDiagnosticCarriesSourceOrdinalAndReason(t *testing.T) {
	dir := t.TempDir()
	siftPath, _ := errorPolicyFixture(t, dir, "on error abort")

	err := runFile(siftPath)
	if err == nil {
		t.Fatal("runFile succeeded, want an abort error")
	}
	msg := err.Error()
	for _, want := range []string{`"in"`, "2", "missing email"} {
		if !strings.Contains(msg, want) {
			t.Errorf("diagnostic %q missing %q (source name / ordinal / reason)", msg, want)
		}
	}
}

// badCellFixture writes a CSV whose middle row has a non-numeric "age"
// cell, plus a .sift program with errPolicyLine as its `on error`
// declaration. The failure originates at the source (coercion), not at
// any stage, so the pipeline is just `in |> filter(...) |> out` — proof
// that ERR-D's failure reaches the driver with no check stage involved.
func badCellFixture(t *testing.T, dir, errPolicyLine, extra string) (siftPath, outPath string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\nTom,not-a-number\nGrace,30\n")
	siftPath = filepath.Join(dir, "prog.sift")
	outPath = filepath.Join(dir, "out.jsonl")
	writeFile(t, siftPath, errPolicyLine+`
source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")
`+extra+`
pipeline main {
  in |> filter(.age >= 0) |> out
}
`)
	return siftPath, outPath
}

// TestERR_D_BadCellAbort: Tom's unparseable age cell aborts the run
// under the default policy — not a panic — after Ada was already
// written.
func TestERR_D_BadCellAbort(t *testing.T) {
	dir := t.TempDir()
	siftPath, outPath := badCellFixture(t, dir, "on error abort", "")

	err := runFile(siftPath)
	if err == nil {
		t.Fatal("runFile succeeded, want an abort error for the bad age cell")
	}
	if !strings.Contains(err.Error(), `cannot parse "not-a-number" as int`) {
		t.Errorf("error = %v, want it to mention the bad cell", err)
	}

	got, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("reading output: %v", readErr)
	}
	if want := `{"name":"Ada","age":42}` + "\n"; string(got) != want {
		t.Errorf("output = %q, want %q (only Ada, before the abort)", got, want)
	}
}

// TestERR_D_BadCellSkip: under `on error skip`, Tom's row is dropped
// silently; Ada and Grace both make it through; the run succeeds.
func TestERR_D_BadCellSkip(t *testing.T) {
	dir := t.TempDir()
	siftPath, outPath := badCellFixture(t, dir, "on error skip", "")

	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	want := `{"name":"Ada","age":42}` + "\n" + `{"name":"Grace","age":30}` + "\n"
	if string(got) != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestERR_D_BadCellRoute: under `on error |> errs`, Tom's row's envelope
// (Stage "csv:age") lands in the error sink, byte-exact, while Ada and
// Grace reach the main sink.
func TestERR_D_BadCellRoute(t *testing.T) {
	dir := t.TempDir()
	siftPath, outPath := badCellFixture(t, dir, "on error |> errs", `sink errs = jsonl("errs.jsonl")`)
	errPath := filepath.Join(dir, "errs.jsonl")

	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading main output: %v", err)
	}
	want := `{"name":"Ada","age":42}` + "\n" + `{"name":"Grace","age":30}` + "\n"
	if string(got) != want {
		t.Errorf("main output = %q, want %q", got, want)
	}

	gotErr, err := os.ReadFile(errPath)
	if err != nil {
		t.Fatalf("reading error sink: %v", err)
	}
	wantErr := `{"source":"in","ordinal":1,"offset":3,"reason":"cannot parse \"not-a-number\" as int","stage":"csv:age"}` + "\n"
	if string(gotErr) != wantErr {
		t.Errorf("error sink = %q, want %q", gotErr, wantErr)
	}
}

// TestERR_E_InfraFatalAbortsEvenUnderSkip: a CSV the reader can't even
// tokenize (a field-count mismatch) must abort the run regardless of
// policy — design-errors.md §2.4 and §7 explicitly call out that `skip`
// must NOT swallow this.
func TestERR_E_InfraFatalAbortsEvenUnderSkip(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nOnlyOneField\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `on error skip
source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> filter(.age >= 0) |> out
}
`)

	err := runFile(siftPath)
	if err == nil {
		t.Fatal("runFile succeeded under on error skip, want the infra-fatal error to still abort")
	}
	if !strings.Contains(err.Error(), "wrong number of fields") {
		t.Errorf("error = %v, want it to surface the underlying reader error", err)
	}
}
