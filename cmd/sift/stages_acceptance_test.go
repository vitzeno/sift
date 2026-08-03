// This file maps directly to design-improvements.md §9's acceptance
// list (S1-A through S4-B): every scenario already has unit-level
// coverage in internal/checker and internal/runtime, added alongside
// each S1-S4 phase. These tests confirm the same scenarios hold through
// the real CLI end to end (runFile — parse, check, build, run), the
// highest-confidence layer, one per acceptance ID for auditability.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestACC_S1_A_DropRemovesColumnAndDownstreamReference: drop(age)
// removes the column from output and schema; a reference to .age
// downstream is a compile error against the reduced schema.
func TestACC_S1_A_DropRemovesColumnAndDownstreamReference(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\n")

	okPath := filepath.Join(dir, "ok.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	writeFile(t, okPath, `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> drop(age) |> out
}
`)
	if err := runFile(okPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if want := `{"name":"Ada"}` + "\n"; string(got) != want {
		t.Errorf("output = %q, want %q", got, want)
	}

	badPath := filepath.Join(dir, "bad.sift")
	writeFile(t, badPath, `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out2.jsonl")

pipeline main {
  in |> drop(age) |> filter(.age >= 18) |> out
}
`)
	err = runFile(badPath)
	if err == nil {
		t.Fatal("runFile succeeded, want a compile error referencing .age after it was dropped")
	}
	if !strings.Contains(err.Error(), `field "age" not in schema`) {
		t.Errorf("error = %v, want it to cite the reduced schema", err)
	}
}

// TestACC_S1_B_SelectOrderAndMissingColumn: select(email, name) yields
// output with columns in that order; a missing column errors citing the
// real set.
func TestACC_S1_B_SelectOrderAndMissingColumn(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age,email\nAda,42,ada@example.com\n")

	okPath := filepath.Join(dir, "ok.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	writeFile(t, okPath, `source in = csv("people.csv", schema: { name: string, age: int, email: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> select(email, name) |> out
}
`)
	if err := runFile(okPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if want := `{"email":"ada@example.com","name":"Ada"}` + "\n"; string(got) != want {
		t.Errorf("output = %q, want %q (email before name)", got, want)
	}

	badPath := filepath.Join(dir, "bad.sift")
	writeFile(t, badPath, `source in = csv("people.csv", schema: { name: string, age: int, email: string })
sink out = jsonl("out2.jsonl")

pipeline main {
  in |> select(emial) |> out
}
`)
	err = runFile(badPath)
	if err == nil {
		t.Fatal("runFile succeeded, want a compile error for the misspelled column")
	}
	if !strings.Contains(err.Error(), `column "emial" not in schema`) {
		t.Errorf("error = %v, want it to cite the real column set", err)
	}
}

// TestACC_S1_C_DroppingPIIColumnSatisfiesSinkRule: a @pii column dropped
// before the sink compiles and runs — no mask needed.
func TestACC_S1_C_DroppingPIIColumnSatisfiesSinkRule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,email\nAda,ada@example.com\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> drop(email) |> out
}
`)
	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if want := `{"name":"Ada"}` + "\n"; string(got) != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestACC_S2_A_RenamePreservesPositionAndType: rename(dob: birth_date)
// renames in place, preserving type and position.
func TestACC_S2_A_RenamePreservesPositionAndType(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,dob,age\nAda,1990-01-01,42\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, dob: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> rename(dob: birth_date) |> out
}
`)
	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	// Same position (2nd field) and type (string) as the original dob.
	if want := `{"name":"Ada","birth_date":"1990-01-01","age":42}` + "\n"; string(got) != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestACC_S2_B_RenamedPIIStillRejectedUnmasked: renaming a @pii column
// and writing it unmasked is still a compile error — the tag survived
// the rename.
func TestACC_S2_B_RenamedPIIStillRejectedUnmasked(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,email\nAda,ada@example.com\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> rename(email: email_addr) |> out
}
`)
	err := runFile(siftPath)
	if err == nil {
		t.Fatal("runFile succeeded, want the renamed column still rejected as unmasked PII")
	}
	if !strings.Contains(err.Error(), `field "email_addr" is @pii and reaches sink`) {
		t.Errorf("error = %v, want it to name the renamed field", err)
	}
}

// TestACC_S3_A_LimitEmitsExactlyFirstRow: limit(1) on the §7 data emits
// exactly the first row.
func TestACC_S3_A_LimitEmitsExactlyFirstRow(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\nTom,15\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> limit(1) |> out
}
`)
	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if want := `{"name":"Ada","age":42}` + "\n"; string(got) != want {
		t.Errorf("output = %q, want %q (Ada only, Tom never reached)", got, want)
	}
}

// TestACC_S3_B_LimitCountsFailedRowsPositionally: with a failing check
// upstream under on error skip, limit(n) counts the failed rows toward
// n (failed rows occupy positions) — asserted via the exact healthy-row
// count reaching the sink.
func TestACC_S3_B_LimitCountsFailedRowsPositionally(t *testing.T) {
	dir := t.TempDir()
	// Ada healthy, Grace fails check (blank email), Tom healthy.
	writeFile(t, filepath.Join(dir, "people.csv"), "name,email\nAda,ada@example.com\nGrace,\nTom,tom@example.com\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	writeFile(t, siftPath, `on error skip

source in = csv("people.csv", schema: { name: string, email: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> check(.email != "", "missing email") |> limit(2) |> out
}
`)
	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	// limit(2) counts Ada (healthy) and Grace (failed) as its two
	// positions, then stops -- Tom is never reached, even though he'd
	// have been healthy. Only Ada reaches the sink.
	if want := `{"name":"Ada","email":"ada@example.com"}` + "\n"; string(got) != want {
		t.Errorf("output = %q, want %q (only Ada; Grace skipped, Tom never reached)", got, want)
	}
}

// TestACC_S4_A_DeclassifyStageCompilesAndRuns: |> hash(email) |> out
// compiles and runs; the output column is present and clean (string, no
// tag).
func TestACC_S4_A_DeclassifyStageCompilesAndRuns(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,email\nAda,ada@example.com\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> hash(email) |> out
}
`)
	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if strings.Contains(string(got), "ada@example.com") {
		t.Errorf("output = %q, want the raw email replaced by its hash", got)
	}
}

// TestACC_S4_B_DeclassifyOnNonPIIColumnRejected: redact(age) where age
// is int is a compile error with a clear message.
func TestACC_S4_B_DeclassifyOnNonPIIColumnRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> redact(age) |> out
}
`)
	err := runFile(siftPath)
	if err == nil {
		t.Fatal("runFile succeeded, want redact(age) rejected — age is not @pii")
	}
	if !strings.Contains(err.Error(), `redact on non-PII column "age"`) {
		t.Errorf("error = %v, want a clear non-PII-target message", err)
	}
}
