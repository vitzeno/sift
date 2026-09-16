// CLI-level coverage for optional fields, adding the cases
// testdata/optional.sift + TestExampleOptional don't cover: a missing
// required column, and a present-but-garbage cell in an optional
// field.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMissingRequiredColumnIsStructuralError: a required column
// missing from the header is a construction-time error naming the
// column and the real header, not a per-row failure.
func TestMissingRequiredColumnIsStructuralError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,phone\nAda,555-0100\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, email: string })
sink out = jsonl("out.jsonl")
pipeline main { in |> out }
`)

	err := runFile(siftPath, false)
	if err == nil {
		t.Fatal("runFile succeeded, want a missing required column error")
	}
	for _, want := range []string{`required column "email" not found`, "name, phone"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to contain %q", err, want)
		}
	}
}

// TestOptionalGarbageCellIsRowFailure: optionality excuses absence,
// never malformed presence. Under `on error skip`, Tom's blank age
// survives as absent (turned into 0 via ?? before the sink), while
// Grace's unparseable one is dropped as a row failure, same as a
// required field's bad cell.
func TestOptionalGarbageCellIsRowFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\nTom,\nGrace,not-a-number\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	writeFile(t, siftPath, `on error skip
source in = csv("people.csv", schema: { name: string, age: int? })
sink out = jsonl("out.jsonl")
pipeline main { in |> map({ ...row, age: .age ?? 0 }) |> out }
`)

	if err := runFile(siftPath, false); err != nil {
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
