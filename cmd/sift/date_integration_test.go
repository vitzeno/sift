// This file rounds out design/date.md's acceptance list with the case
// testdata/date.sift + TestExampleDate can't show: a cell that fails to
// parse against its declared date format, routed as a row failure by
// the active error policy, driven through the real CLI like the
// column-aliases phase's integration tests.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDATE_C_BadCellIsRowFailure is DATE-C: a cell that names an
// impossible calendar date is a row failure, not a construction error
// and not a panic, routed to the error sink like any other bad cell.
func TestDATE_C_BadCellIsRowFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "signups.csv"), "name,signup_date\nAda,2026-01-05\nGrace,2026-02-30\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	errPath := filepath.Join(dir, "errors.jsonl")
	writeFile(t, siftPath, `on error |> errors

source in  = csv("signups.csv", schema: { name: string, signup_date: date })
sink   out = jsonl("out.jsonl")
sink   errors = jsonl("errors.jsonl")

pipeline main { in |> out }
`)

	if err := runFile(siftPath, false); err != nil {
		t.Fatalf("runFile error: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading %s: %v", outPath, err)
	}
	want := `{"name":"Ada","signup_date":"2026-01-05"}` + "\n"
	if string(got) != want {
		t.Errorf("out.jsonl = %q, want %q", got, want)
	}

	errGot, err := os.ReadFile(errPath)
	if err != nil {
		t.Fatalf("reading %s: %v", errPath, err)
	}
	if !strings.Contains(string(errGot), `cannot parse \"2026-02-30\" as date`) {
		t.Errorf("errors.jsonl = %q, want it to mention the impossible calendar date", errGot)
	}
}

// TestDATE_UnknownFormatEntryFieldIsHarmless confirms a formats kwarg
// naming a field the schema doesn't have (a typo, or a field that isn't
// date-typed) is silently unused, the same additive-and-optional shape
// columns: already has (design/column-aliases.md §3), not a
// construction error.
func TestDATE_UnknownFormatEntryFieldIsHarmless(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "signups.csv"), "name,signup_date\nAda,2026-01-05\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	writeFile(t, siftPath, `source in = csv("signups.csv",
  schema:  { name: string, signup_date: date },
  formats: { nonexistent: "02/01/2006" }
)
sink out = jsonl("out.jsonl")
pipeline main { in |> out }
`)

	if err := runFile(siftPath, false); err != nil {
		t.Fatalf("runFile error: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading %s: %v", outPath, err)
	}
	want := `{"name":"Ada","signup_date":"2026-01-05"}` + "\n"
	if string(got) != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}
