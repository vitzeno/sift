// CLI-level coverage for column aliases, adding the case
// testdata/columns.sift + TestExampleColumns can't show: a required
// column that resolves neither by alias nor by identifier.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRequiredColumnUnresolvedIsStructuralError is A-3: no alias
// entry and no identifier match for a required field is a construction
// error naming the field and the real header, not a per-row failure.
func TestRequiredColumnUnresolvedIsStructuralError(t *testing.T) {
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

	err := runFile(siftPath, false)
	if err == nil {
		t.Fatal("runFile succeeded, want a missing required column error")
	}
	for _, want := range []string{`required column "txn_date" not found`, "Transaction ID, Amount"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to contain %q", err, want)
		}
	}
}

// TestOptionalAliasedColumnMissingIsAbsent confirms an Optional
// field with an alias whose header isn't in the file at all resolves to
// absent, not a construction error, and the run completes normally.
func TestOptionalAliasedColumnMissingIsAbsent(t *testing.T) {
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

	if err := runFile(siftPath, false); err != nil {
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
