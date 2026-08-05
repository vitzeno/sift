// This file rounds out design/datetime.md's acceptance list with the
// cases testdata/datetime.sift + TestExampleDateTime can't show: a cell
// naming an impossible time component routed as a row failure, and the
// compile error that proves date and datetime never mix, driven through
// the real CLI like the date phase's own integration tests.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDT_C_BadCellIsRowFailure is DT-C: a cell that matches its declared
// format but names an impossible time component (hour 25) is a row
// failure, not a construction error and not a panic, routed to the error
// sink like any other bad cell.
func TestDT_C_BadCellIsRowFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "events.csv"), "name,occurred_at\nAda,2026-07-31T04:10:25\nGrace,2026-07-31T25:10:25\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	errPath := filepath.Join(dir, "errors.jsonl")
	writeFile(t, siftPath, `on error |> errors

source in     = csv("events.csv", schema: { name: string, occurred_at: datetime })
sink   out    = jsonl("out.jsonl")
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
	want := `{"name":"Ada","occurred_at":"2026-07-31T04:10:25"}` + "\n"
	if string(got) != want {
		t.Errorf("out.jsonl = %q, want %q", got, want)
	}

	errGot, err := os.ReadFile(errPath)
	if err != nil {
		t.Fatalf("reading %s: %v", errPath, err)
	}
	if !strings.Contains(string(errGot), `cannot parse \"2026-07-31T25:10:25\" as datetime`) {
		t.Errorf("errors.jsonl = %q, want it to mention the impossible hour", errGot)
	}
}

// TestDT_I_DateAndDateTimeNeverMix is DT-I: a schema with both a date
// field and a datetime field, compared directly, is a compile error --
// confirms design/datetime.md §2's "never mix" decision is actually
// enforced through the real CLI, not just at the checker's unit level.
func TestDT_I_DateAndDateTimeNeverMix(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mixed.csv"), "signup_date,last_login\n2026-01-05,2026-02-10T09:30:00\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `source in = csv("mixed.csv", schema: { signup_date: date, last_login: datetime })
sink   out = jsonl("out.jsonl")
pipeline main { in |> filter(.signup_date == .last_login) |> out }
`)

	err := runFile(siftPath, false)
	if err == nil {
		t.Fatal("runFile succeeded, want a compile error: date and datetime never mix")
	}
	if !strings.Contains(err.Error(), "cannot compare date and datetime") {
		t.Errorf("error = %v, want it to mention date and datetime", err)
	}
}
