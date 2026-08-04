// This file rounds out design/routing.md's acceptance list with the
// cases examples/routing.sift + TestExampleRouting doesn't cover: an
// overlapping-predicate first-match proof, a missing else compile error,
// exhaustive row conservation across sinks and discard, and route
// composed with error routing — all driven through the real CLI entry
// point the same way the errors/multisink/optional integration tests do.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRT_A_FirstMatchWinsOverOverlap: a row satisfying two branches'
// predicates lands in the earlier one's sink only.
func TestRT_A_FirstMatchWinsOverOverlap(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\n")
	siftPath := filepath.Join(dir, "prog.sift")
	firstPath := filepath.Join(dir, "first.jsonl")
	secondPath := filepath.Join(dir, "second.jsonl")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, age: int })
sink first  = jsonl("first.jsonl")
sink second = jsonl("second.jsonl")
pipeline main {
  in |> route {
    .age >= 18 => first,
    .age >= 0  => second,
    else       => second,
  }
}
`)
	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}

	gotFirst, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatalf("reading first sink: %v", err)
	}
	if want := `{"name":"Ada","age":42}` + "\n"; string(gotFirst) != want {
		t.Errorf("first sink = %q, want %q", gotFirst, want)
	}

	// Every declared sink is opened at build time regardless of whether
	// any row ever reaches it, so second.jsonl exists but must be empty.
	gotSecond, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatalf("reading second sink: %v", err)
	}
	if len(gotSecond) != 0 {
		t.Errorf("second sink = %q, want empty (the first branch already matched)", gotSecond)
	}
}

// TestRT_B_MissingElseIsCompileError: a route with no else branch fails
// to compile, naming exactly what's missing.
func TestRT_B_MissingElseIsCompileError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,region\nAda,EU\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, region: string })
sink out = jsonl("out.jsonl")
pipeline main {
  in |> route {
    .region == "EU" => out,
  }
}
`)
	err := runFile(siftPath)
	if err == nil {
		t.Fatal("runFile succeeded, want a compile error for the missing else branch")
	}
	want := `route is not total; add an 'else' branch (use 'else => discard' to drop unmatched rows explicitly)`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to contain %q", err, want)
	}
}

// TestRT_D_ExhaustiveCoverageConservesRowCount: every input row lands in
// exactly one place across the target sinks and discard — total row
// count is conserved, none duplicated, none silently dropped except
// through the explicit discard branch.
func TestRT_D_ExhaustiveCoverageConservesRowCount(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"),
		"name,region\nAda,EU\nTom,US\nGrace,APAC\nLiam,EU\nNina,US\nOwen,AF\n")
	siftPath := filepath.Join(dir, "prog.sift")
	euPath := filepath.Join(dir, "eu.jsonl")
	usPath := filepath.Join(dir, "us.jsonl")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, region: string })
sink eu_sink = jsonl("eu.jsonl")
sink us_sink = jsonl("us.jsonl")
pipeline main {
  in |> route {
    .region == "EU" => eu_sink,
    .region == "US" => us_sink,
    else            => discard,
  }
}
`)
	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}

	gotEU, err := os.ReadFile(euPath)
	if err != nil {
		t.Fatalf("reading eu sink: %v", err)
	}
	gotUS, err := os.ReadFile(usPath)
	if err != nil {
		t.Fatalf("reading us sink: %v", err)
	}

	wantEU := `{"name":"Ada","region":"EU"}` + "\n" + `{"name":"Liam","region":"EU"}` + "\n"
	wantUS := `{"name":"Tom","region":"US"}` + "\n" + `{"name":"Nina","region":"US"}` + "\n"
	if string(gotEU) != wantEU {
		t.Errorf("eu sink = %q, want %q", gotEU, wantEU)
	}
	if string(gotUS) != wantUS {
		t.Errorf("us sink = %q, want %q", gotUS, wantUS)
	}
	// 6 input rows: 2 EU + 2 US written, Grace (APAC) and Owen (AF)
	// silently discarded — no row duplicated, none unaccounted for.
	totalWritten := strings.Count(string(gotEU), "\n") + strings.Count(string(gotUS), "\n")
	if totalWritten != 4 {
		t.Errorf("total rows written = %d, want 4 (6 input - 2 discarded)", totalWritten)
	}
}

// TestRT_E_RouteComposesWithErrorRouting: a failed row goes to the error
// sink under `on error |> errs` and is never evaluated by a route branch
// — the Fail cascade runs first regardless of which terminal production
// the program uses (design-routing.md §3).
func TestRT_E_RouteComposesWithErrorRouting(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\nTom,not-a-number\nGrace,15\n")
	siftPath := filepath.Join(dir, "prog.sift")
	adultPath := filepath.Join(dir, "adult.jsonl")
	minorPath := filepath.Join(dir, "minor.jsonl")
	errPath := filepath.Join(dir, "errs.jsonl")
	writeFile(t, siftPath, `on error |> errs
source in = csv("people.csv", schema: { name: string, age: int })
sink adult = jsonl("adult.jsonl")
sink minor = jsonl("minor.jsonl")
sink errs  = jsonl("errs.jsonl")
pipeline main {
  in |> route {
    .age >= 18 => adult,
    else       => minor,
  }
}
`)
	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}

	gotAdult, err := os.ReadFile(adultPath)
	if err != nil {
		t.Fatalf("reading adult sink: %v", err)
	}
	if want := `{"name":"Ada","age":42}` + "\n"; string(gotAdult) != want {
		t.Errorf("adult sink = %q, want %q", gotAdult, want)
	}

	gotMinor, err := os.ReadFile(minorPath)
	if err != nil {
		t.Fatalf("reading minor sink: %v", err)
	}
	if want := `{"name":"Grace","age":15}` + "\n"; string(gotMinor) != want {
		t.Errorf("minor sink = %q, want %q", gotMinor, want)
	}

	gotErr, err := os.ReadFile(errPath)
	if err != nil {
		t.Fatalf("reading error sink: %v", err)
	}
	if !strings.Contains(string(gotErr), "not-a-number") {
		t.Errorf("error sink = %q, want it to carry Tom's coercion failure", gotErr)
	}
	if strings.Contains(string(gotErr), "Tom") {
		t.Error("error sink envelope must never carry the failed row's own fields")
	}
}

// TestRT_F_DuplicateSinkAcrossBranchesReceivesUnion: the same sink named
// in two branches receives the union of every row either branch matched
// — allowed for route, unlike broadcast's duplicate-sink error.
func TestRT_F_DuplicateSinkAcrossBranchesReceivesUnion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,region\nAda,EU\nTom,US\nGrace,APAC\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, region: string })
sink out = jsonl("out.jsonl")
pipeline main {
  in |> route {
    .region == "EU" => out,
    .region == "US" => out,
    else            => out,
  }
}
`)
	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	want := `{"name":"Ada","region":"EU"}` + "\n" +
		`{"name":"Tom","region":"US"}` + "\n" +
		`{"name":"Grace","region":"APAC"}` + "\n"
	if string(got) != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}
