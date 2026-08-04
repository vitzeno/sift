// This file rounds out design/column-aliases.md's acceptance list with
// the case testdata/columns.sift + TestExampleColumns can't show: a
// required column that resolves neither by alias nor by identifier,
// driven through the real CLI like the optional-fields and xlsx phases'
// integration tests.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCOL_A3_RequiredColumnUnresolvedIsStructuralError is A-3: no alias
// entry and no identifier match for a required field is a construction
// error naming the field and the real header, not a per-row failure.
func TestCOL_A3_RequiredColumnUnresolvedIsStructuralError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "transactions.csv"), "Transaction ID,Amount\n1001,42.50\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `source in = csv("transactions.csv",
  schema:  { txn_id: int, txn_date: string },
  columns: { txn_id: "Transaction ID", txn_date: "Date" }
)
sink out = jsonl("out.jsonl")
pipeline main { in |> out }
`)

	err := runFile(siftPath)
	if err == nil {
		t.Fatal("runFile succeeded, want a missing required column error")
	}
	for _, want := range []string{`required column "txn_date" not found`, "Transaction ID, Amount"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to contain %q", err, want)
		}
	}
}

// TestCOL_D_OptionalAliasedColumnMissingIsAbsent is design/column-aliases.md's
// acceptance case D: an Optional field with an alias whose header isn't
// in the file at all resolves to absent, not a construction error, and
// the run completes normally.
func TestCOL_D_OptionalAliasedColumnMissingIsAbsent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "transactions.csv"), "Transaction ID,Amount\n1001,42.50\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	writeFile(t, siftPath, `source in = csv("transactions.csv",
  schema:  { txn_id: int, txn_date: string? },
  columns: { txn_id: "Transaction ID", txn_date: "Date" }
)
sink out = jsonl("out.jsonl")
pipeline main { in |> map({ ...row, txn_date: .txn_date ?? "unknown" }) |> out }
`)

	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	want := `{"txn_id":1001,"txn_date":"unknown"}` + "\n"
	if string(got) != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}
