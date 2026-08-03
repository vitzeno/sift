// This file runs every example program in examples/ through the real
// CLI entry point (runFile) and asserts on its output byte-exact — the
// same discipline complex_test.go used to apply to one big combined
// program, now spread across examples/'s per-feature examples (filter,
// map, check, pii, named-segment) and the new design-errors.md examples
// (on-error-abort/skip/route, bad-cell). Every example here doubles as
// documentation: each demonstrates exactly one language feature or error
// policy, in isolation, matching CLAUDE.md's own examples/ convention
// (".sift programs + input / expected-output fixtures").
package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vitzeno/sift/internal/runtime"
)

// runExample runs examples/name.sift and returns name_out.jsonl's
// contents, cleaning up every file the program writes (which, thanks to
// script-relative path resolution, land directly in examples/ next to
// the .sift file itself — not in some test-private temp directory).
func runExample(t *testing.T, name string, extraOutputs ...string) []byte {
	t.Helper()
	siftPath := filepath.Join("..", "..", "examples", name+".sift")
	outPath := filepath.Join("..", "..", "examples", name+"_out.jsonl")
	t.Cleanup(func() { os.Remove(outPath) })
	for _, extra := range extraOutputs {
		path := filepath.Join("..", "..", "examples", extra)
		t.Cleanup(func() { os.Remove(path) })
	}

	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile(%s) error: %v", siftPath, err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading %s: %v", outPath, err)
	}
	return got
}

func TestExampleFilter(t *testing.T) {
	got := runExample(t, "filter")
	want := `{"name":"Ada","age":42,"active":true}
{"name":"Liam","age":25,"active":true}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

func TestExampleMap(t *testing.T) {
	got := runExample(t, "map")
	want := `{"name":"ADA","age":42,"balance":250.5,"balance_ok":true,"age_next_year":43}
{"name":"TOM","age":15,"balance":50,"balance_ok":false,"age_next_year":16}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

func TestExampleCheck(t *testing.T) {
	got := runExample(t, "check")
	want := `{"name":"Ada","email":"ada@example.com"}
{"name":"Tom","email":"tom@example.com"}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

func TestExamplePII(t *testing.T) {
	got := runExample(t, "pii")
	want := `{"name":"Ada","email":"***************"}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

func TestExampleSelect(t *testing.T) {
	got := runExample(t, "select")
	want := `{"email":"ada@example.com","name":"Ada","plan":"pro"}
{"email":"tom@example.com","name":"Tom","plan":"free"}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

func TestExampleDrop(t *testing.T) {
	got := runExample(t, "drop")
	want := `{"name":"Ada","department":"Engineering"}
{"name":"Tom","department":"Sales"}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

func TestExampleRename(t *testing.T) {
	got := runExample(t, "rename")
	want := `{"name":"Ada","birth_date":"1990-01-01","email":"***************"}
{"name":"Tom","birth_date":"2001-05-12","email":"***************"}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

func TestExampleLimitOffset(t *testing.T) {
	got := runExample(t, "limit-offset")
	want := `{"name":"Grace","event":"purchase"}
{"name":"Liam","event":"logout"}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

func TestExampleDeclassify(t *testing.T) {
	got := runExample(t, "declassify")
	want := `{"ticket_id":1,"email":"b5fc85e55755f9e0d030a10ab4429b6b2944855f9a0d60077fe832becbc41d72","phone":"[REDACTED]","subject":"Billing question"}
{"ticket_id":2,"email":"72bb75a959e1785b79ffe7230eaeec25880707a91b4a4f98330fc1510bd40e03","phone":"[REDACTED]","subject":"Login issue"}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

// TestExampleCustomerExport runs the composite real-world pipeline that
// chains every stage design-improvements.md added — drop, rename, hash,
// mask, select, and limit — in the order a GDPR-safe analytics extract
// would actually use them.
func TestExampleCustomerExport(t *testing.T) {
	got := runExample(t, "customer-export")
	want := `{"id":1,"name":"Ada Lovelace","email":"b5fc85e55755f9e0d030a10ab4429b6b2944855f9a0d60077fe832becbc41d72","phone":"********","plan":"pro","joined_at":"2024-01-15"}
{"id":2,"name":"Tom Reed","email":"72bb75a959e1785b79ffe7230eaeec25880707a91b4a4f98330fc1510bd40e03","phone":"********","plan":"free","joined_at":"2024-02-20"}
{"id":3,"name":"Grace Hopper","email":"b533d4547eaa5a0fa955965a1ca393ccd2ea013032a105726f232eb41bddc4fa","phone":"********","plan":"pro","joined_at":"2024-03-05"}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

func TestExampleNamedSegment(t *testing.T) {
	got := runExample(t, "named-segment")
	want := `{"name":"Ada","email":"ada@example.com"}
{"name":"Tom","email":"tom@example.com"}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

// TestExampleOnErrorAbort confirms on-error-abort.sift's own claim:
// Grace's blank email aborts the run, and Liam (after her in the CSV)
// is never even reached — only Ada's row, read before the failure,
// lands in the sink.
func TestExampleOnErrorAbort(t *testing.T) {
	siftPath := filepath.Join("..", "..", "examples", "on-error-abort.sift")
	outPath := filepath.Join("..", "..", "examples", "on-error-abort_out.jsonl")
	t.Cleanup(func() { os.Remove(outPath) })

	err := runFile(siftPath)
	if err == nil {
		t.Fatal("runFile succeeded, want the default abort policy to stop the run")
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
	want := `{"name":"Ada","age":42,"email":"ada@example.com"}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s (only Ada, written before the abort)", got, want)
	}
}

func TestExampleOnErrorSkip(t *testing.T) {
	got := runExample(t, "on-error-skip")
	want := `{"name":"Ada","age":42,"email":"ada@example.com"}
{"name":"Liam","age":25,"email":"liam@example.com"}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

// TestExampleOnErrorRoute checks both sinks on-error-route.sift writes
// to: healthy rows in the main sink, Grace's envelope — provenance and
// reason, never her raw fields — in the error sink.
func TestExampleOnErrorRoute(t *testing.T) {
	got := runExample(t, "on-error-route", "on-error-route_errors.jsonl")
	want := `{"name":"Ada","age":42,"email":"ada@example.com"}
{"name":"Liam","age":25,"email":"liam@example.com"}
`
	if string(got) != want {
		t.Errorf("main output =\n%s\nwant\n%s", got, want)
	}

	errPath := filepath.Join("..", "..", "examples", "on-error-route_errors.jsonl")
	gotErr, err := os.ReadFile(errPath)
	if err != nil {
		t.Fatalf("reading error sink: %v", err)
	}
	wantErr := `{"source":"in","ordinal":2,"offset":4,"reason":"missing email","stage":"check"}
`
	if string(gotErr) != wantErr {
		t.Errorf("error sink =\n%s\nwant\n%s", gotErr, wantErr)
	}
}

// TestExampleBroadcast is design-multisink.md MS-A: `|> warehouse, audit`
// writes byte-identical masked output to both sinks.
func TestExampleBroadcast(t *testing.T) {
	got := runExample(t, "broadcast", "broadcast_audit.jsonl")
	want := `{"name":"Ada","email":"***************"}
{"name":"Tom","email":"***************"}
`
	if string(got) != want {
		t.Errorf("warehouse output =\n%s\nwant\n%s", got, want)
	}

	auditPath := filepath.Join("..", "..", "examples", "broadcast_audit.jsonl")
	gotAudit, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("reading audit sink: %v", err)
	}
	if string(gotAudit) != string(got) {
		t.Errorf("audit output =\n%s\nwant byte-identical to warehouse output\n%s", gotAudit, got)
	}
}

func TestExampleBadCell(t *testing.T) {
	got := runExample(t, "bad-cell")
	want := `{"name":"Ada","age":42}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}
