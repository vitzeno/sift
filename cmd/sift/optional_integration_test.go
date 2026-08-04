// This file rounds out design/optional-fields.md's acceptance list with
// the cases the examples/optional.sift + TestExampleOptional pair doesn't
// cover: a missing required column (OF-C) and a present-but-garbage cell
// in an optional field (OF-D), both driven through the real CLI entry
// point the same way the errors and xlsx phases' integration tests do.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOF_C_MissingRequiredColumnIsStructuralError: a required column
// absent from the header is a construction-time error naming the column
// and the real header, not a per-row failure.
func TestOF_C_MissingRequiredColumnIsStructuralError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,phone\nAda,555-0100\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, email: string })
sink out = jsonl("out.jsonl")
pipeline main { in |> out }
`)

	err := runFile(siftPath)
	if err == nil {
		t.Fatal("runFile succeeded, want a missing required column error")
	}
	for _, want := range []string{`required column "email" not found`, "name, phone"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to contain %q", err, want)
		}
	}
}

// TestOF_D_OptionalGarbageCellIsRowFailure: optionality excuses absence,
// never malformed presence — under `on error skip`, Tom's blank age
// survives as absent (discharged to 0 via ?? before the sink) while
// Grace's unparseable one is dropped as a row failure, exactly like a
// required field's bad cell would be.
func TestOF_D_OptionalGarbageCellIsRowFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\nTom,\nGrace,not-a-number\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	writeFile(t, siftPath, `on error skip
source in = csv("people.csv", schema: { name: string, age: int? })
sink out = jsonl("out.jsonl")
pipeline main { in |> map({ ...row, age: .age ?? 0 }) |> out }
`)

	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	want := `{"name":"Ada","age":42}` + "\n" + `{"name":"Tom","age":0}` + "\n"
	if string(got) != want {
		t.Errorf("output = %q, want %q (Grace's garbage cell skipped, Tom's absence survives)", got, want)
	}
}
