package main

import (
	"os"
	"testing"
)

// TestRunComplexFixtureExercisesEveryV0Feature runs testdata/complex.sift
// through the real CLI entry point (runFile), the same function `sift
// run` calls — proving every v0 feature composes correctly in one
// program: all four scalar kinds, a @pii field masked through a nested
// call (mask(lower(trim(.email)))), a non-declassifying transform
// (upper(.name)), a named segment (clean) inlined by the checker, check
// passing without aborting, filter combining a numeric comparison with a
// bare bool field via &&, and a second map both overriding nothing and
// adding two brand-new fields (a comparison and an arithmetic
// expression). It also exercises the script-relative path resolution
// fixed for `run`: complex.sift refers to "complex.csv" and
// "complex_out.jsonl" by bare name, both resolved against testdata/,
// regardless of this test's own working directory.
func TestRunComplexFixtureExercisesEveryV0Feature(t *testing.T) {
	const siftPath = "../../testdata/complex.sift"
	const outPath = "../../testdata/complex_out.jsonl"
	t.Cleanup(func() { os.Remove(outPath) })

	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	// Tom (age 15) fails the age check; Grace (active=false) fails the
	// && check. Only Ada and Liam survive, in their original order.
	want := `{"name":"ADA","age":42,"balance":250.5,"active":true,"email":"***************","balance_ok":true,"age_next_year":43}
{"name":"LIAM","age":25,"balance":500,"active":true,"email":"****************","balance_ok":true,"age_next_year":26}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}
