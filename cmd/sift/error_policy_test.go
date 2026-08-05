package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vitzeno/sift/internal/runtime"
)

// errorPolicyFixture writes people.csv (one row with a blank email) and
// a .sift program with errPolicyLine as its `on error` declaration
// (empty for the default). Returns the paths sift run needs.
func errorPolicyFixture(t *testing.T, dir, errPolicyLine string) (siftPath, outPath string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age,email\nAda,42,ada@example.com\nTom,15,tom@example.com\nGrace,30,\n")
	siftPath = filepath.Join(dir, "prog.sift")
	outPath = filepath.Join(dir, "out.jsonl")
	writeFile(t, siftPath, errPolicyLine+`
source in = csv("people.csv", schema: { name: string, age: int, email: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> check(.email != "", "missing email") |> out
}
`)
	return siftPath, outPath
}

// TestRunOnErrorAbort is ERR-A through the real CLI: Tom is dropped by
// filter (age 15, healthy), Grace fails check (blank email). The run
// must abort with a *runtime.FailureError naming the reason, and must
// not have written anything for Grace.
func TestRunOnErrorAbort(t *testing.T) {
	dir := t.TempDir()
	siftPath, outPath := errorPolicyFixture(t, dir, "on error abort")

	err := runFile(siftPath, false)
	if err == nil {
		t.Fatal("runFile succeeded, want a FailureError")
	}
	var fe *runtime.FailureError
	if !errors.As(err, &fe) {
		t.Fatalf("error type = %T, want *runtime.FailureError", err)
	}
	if fe.Fail.Reason != "missing email" {
		t.Errorf("Reason = %q, want %q", fe.Fail.Reason, "missing email")
	}

	got, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("reading output: %v", readErr)
	}
	want := `{"name":"Ada","age":42,"email":"ada@example.com"}` + "\n"
	if string(got) != want {
		t.Errorf("output = %q, want %q (only Ada, written before the abort)", got, want)
	}
}

// TestRunOnErrorSkip is ERR-B through the real CLI: under `on error
// skip`, the same program drops Grace's row, writes Ada's, and exits
// with no error.
func TestRunOnErrorSkip(t *testing.T) {
	dir := t.TempDir()
	siftPath, outPath := errorPolicyFixture(t, dir, "on error skip")

	if err := runFile(siftPath, false); err != nil {
		t.Fatalf("runFile error: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	want := `{"name":"Ada","age":42,"email":"ada@example.com"}` + "\n"
	if string(got) != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestRunOnErrorRoute is ERR-C through the real CLI: healthy rows reach
// the main sink, and Grace's failure reaches the declared error sink as
// exactly one envelope line, asserted byte-exact per design-errors.md
// §7.
func TestRunOnErrorRoute(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age,email\nAda,42,ada@example.com\nTom,15,tom@example.com\nGrace,30,\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	errPath := filepath.Join(dir, "errs.jsonl")
	writeFile(t, siftPath, `on error |> errs
source in = csv("people.csv", schema: { name: string, age: int, email: string })
sink out = jsonl("out.jsonl")
sink errs = jsonl("errs.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> check(.email != "", "missing email") |> out
}
`)

	if err := runFile(siftPath, false); err != nil {
		t.Fatalf("runFile error: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading main output: %v", err)
	}
	want := `{"name":"Ada","age":42,"email":"ada@example.com"}` + "\n"
	if string(got) != want {
		t.Errorf("main output = %q, want %q", got, want)
	}

	gotErr, err := os.ReadFile(errPath)
	if err != nil {
		t.Fatalf("reading error sink output: %v", err)
	}
	wantErr := `{"source":"in","ordinal":2,"offset":4,"reason":"missing email","stage":"check"}` + "\n"
	if string(gotErr) != wantErr {
		t.Errorf("error sink output = %q, want %q", gotErr, wantErr)
	}
}

// TestRunOnErrorRouteToUndefinedSinkFailsAtCheckTime confirms a bad
// route target is caught by the checker before anything runs, not at
// Build/Run time.
func TestRunOnErrorRouteToUndefinedSinkFailsAtCheckTime(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `on error |> nope
source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> out
}
`)

	err := runFile(siftPath, false)
	if err == nil {
		t.Fatal("runFile succeeded, want an undefined-route-target error")
	}
}
