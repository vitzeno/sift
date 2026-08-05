// This file runs every example program in testdata/ through the real CLI
// entry point (runFile) and asserts its output byte-exact. complex_test.go
// used to apply this discipline to one big combined program; now it's
// spread across testdata/'s per-feature examples (filter, map, check, pii,
// named-segment) plus the design-errors.md examples (on-error-abort/skip/
// route, bad-cell). Each example demonstrates exactly one language feature
// or error policy.
//
// testdata/ holds a copy of every fixture actually exercised by a Go test
// (this file, plus internal/runtime and internal/format); examples/ is the
// same set of programs kept for the tutorial in examples/README.md and is
// never read by a test. The two are expected to stay identical for any
// fixture that lives in both — if you change one, change the other.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vitzeno/sift/internal/runtime"
)

// runExample runs testdata/name.sift and returns name_out.jsonl's
// contents, cleaning up every file the program writes. Script-relative
// path resolution puts those files directly in testdata/ next to the
// .sift file, not in a test-private temp directory.
func runExample(t *testing.T, name string, extraOutputs ...string) []byte {
	t.Helper()
	siftPath := filepath.Join("..", "..", "testdata", name+".sift")
	outPath := filepath.Join("..", "..", "testdata", name+"_out.jsonl")
	t.Cleanup(func() { os.Remove(outPath) })
	for _, extra := range extraOutputs {
		path := filepath.Join("..", "..", "testdata", extra)
		t.Cleanup(func() { os.Remove(path) })
	}

	if err := runFile(siftPath, false); err != nil {
		t.Fatalf("runFile(%s, false) error: %v", siftPath, err)
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

// TestExampleCustomerExport runs a composite real-world pipeline chaining
// every stage design-improvements.md added (drop, rename, hash, mask,
// select, limit) in the order a GDPR-safe analytics extract would use them.
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

// TestExampleSegments is design/segments.md's flagship demo: `scrub(col)`
// reused across two different @pii columns, and `adults(min: int)` called
// with a literal, composed together in one `|>` chain.
func TestExampleSegments(t *testing.T) {
	got := runExample(t, "segments")
	want := `{"name":"Ada","age":42,"email":"********","backup_email":"*********"}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

// TestExampleOnErrorAbort confirms on-error-abort.sift's claim: Grace's
// blank email aborts the run, Liam (after her in the CSV) is never
// reached, and only Ada's row, read before the failure, lands in the sink.
func TestExampleOnErrorAbort(t *testing.T) {
	siftPath := filepath.Join("..", "..", "testdata", "on-error-abort.sift")
	outPath := filepath.Join("..", "..", "testdata", "on-error-abort_out.jsonl")
	t.Cleanup(func() { os.Remove(outPath) })

	err := runFile(siftPath, false)
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

// TestExampleOnErrorRoute checks both sinks on-error-route.sift writes to:
// healthy rows in the main sink, and Grace's envelope (provenance and
// reason, never her raw fields) in the error sink.
func TestExampleOnErrorRoute(t *testing.T) {
	got := runExample(t, "on-error-route", "on-error-route_errors.jsonl")
	want := `{"name":"Ada","age":42,"email":"ada@example.com"}
{"name":"Liam","age":25,"email":"liam@example.com"}
`
	if string(got) != want {
		t.Errorf("main output =\n%s\nwant\n%s", got, want)
	}

	errPath := filepath.Join("..", "..", "testdata", "on-error-route_errors.jsonl")
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

	auditPath := filepath.Join("..", "..", "testdata", "broadcast_audit.jsonl")
	gotAudit, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("reading audit sink: %v", err)
	}
	if string(gotAudit) != string(got) {
		t.Errorf("audit output =\n%s\nwant byte-identical to warehouse output\n%s", gotAudit, got)
	}
}

// TestExampleRouting is design-routing.md's own example end to end: mask
// once, then route each row to exactly one of three sinks by region.
// Ada's EU row lands only in the primary output; Tom and Grace land in
// the other two. Never more than one write per row.
func TestExampleRouting(t *testing.T) {
	got := runExample(t, "routing", "routing_us.jsonl", "routing_rest.jsonl")
	want := `{"name":"Ada","email":"***************","region":"EU"}
`
	if string(got) != want {
		t.Errorf("eu output =\n%s\nwant\n%s", got, want)
	}

	usPath := filepath.Join("..", "..", "testdata", "routing_us.jsonl")
	gotUS, err := os.ReadFile(usPath)
	if err != nil {
		t.Fatalf("reading us sink: %v", err)
	}
	wantUS := `{"name":"Tom","email":"***************","region":"US"}
`
	if string(gotUS) != wantUS {
		t.Errorf("us output =\n%s\nwant\n%s", gotUS, wantUS)
	}

	restPath := filepath.Join("..", "..", "testdata", "routing_rest.jsonl")
	gotRest, err := os.ReadFile(restPath)
	if err != nil {
		t.Fatalf("reading rest sink: %v", err)
	}
	wantRest := `{"name":"Grace","email":"*****************","region":"APAC"}
`
	if string(gotRest) != wantRest {
		t.Errorf("rest output =\n%s\nwant\n%s", gotRest, wantRest)
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

// TestExamplePIIOptional is OF-G end to end: email is both optional and
// @pii; one expression (`mask(.email ?? "n/a")`) discharges both tags
// before the sink.
func TestExamplePIIOptional(t *testing.T) {
	got := runExample(t, "pii-optional")
	want := `{"name":"Ada","email":"***************"}
{"name":"Tom","email":"***"}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

// TestExampleOptional is OF-A and OF-E end to end: a blank cell against
// an optional field reads as an absent value, not a row failure, and ??
// discharges it with a default before the sink.
func TestExampleOptional(t *testing.T) {
	got := runExample(t, "optional")
	want := `{"name":"Ada","phone":"555-0100"}
{"name":"Tom","phone":"n/a"}
{"name":"Nina","phone":"555-0199"}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

// TestExampleETL exercises etl.sift end to end: a reusable parameterless
// segment (validate), a plain filter, a parameterized scalar segment
// (eligible), map, drop, rename, hash, a parameterized column segment
// (scrub), select, offset/limit, and a terminal broadcast to two sinks.
// Nearly every language feature, composed into one pipeline.
func TestExampleETL(t *testing.T) {
	got := runExample(t, "etl", "etl_audit.jsonl")
	want := `{"id":4,"name":"Liam Wu","email":"d9c57089f04f2b2e9cd8abcc2e1088afc708fcf60b842d42ab3dc3d3c153e13e","phone":"********","age":25,"amount":500,"plan":"FREE","high_value":true,"joined_at":"2024-04-10"}
{"id":5,"name":"Nina Simone","email":"cec43edb6a1681336ab87fa21ea576e83450826e3cc05f2ca73128e7fd69745f","phone":"********","age":29,"amount":120,"plan":"PRO","high_value":false,"joined_at":"2024-05-12"}
`
	if string(got) != want {
		t.Errorf("out output =\n%s\nwant\n%s", got, want)
	}

	auditPath := filepath.Join("..", "..", "testdata", "etl_audit.jsonl")
	gotAudit, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("reading audit sink: %v", err)
	}
	if string(gotAudit) != string(got) {
		t.Errorf("audit output =\n%s\nwant byte-identical to out output\n%s", gotAudit, got)
	}
}

// TestExampleETLErrors is etl-errors.sift: the same pipeline as etl.sift,
// under `on error |> errors`. Zoe's blank email fails validate's check;
// Max's amount cell won't coerce to double at all, so it fails at the
// source before validate even runs. Both divert to the errors sink as a
// fixed envelope, never their raw fields, while every other row still
// flows through the full pipeline (segments, declassifiers, projection)
// and reaches both out and audit.
func TestExampleETLErrors(t *testing.T) {
	got := runExample(t, "etl-errors", "etl-errors_audit.jsonl", "etl-errors_errors.jsonl")
	want := `{"id":1,"name":"Ada Lovelace","email":"b5fc85e55755f9e0d030a10ab4429b6b2944855f9a0d60077fe832becbc41d72","phone":"********","age":42,"amount":250.5,"plan":"PRO","high_value":false,"joined_at":"2024-01-15"}
{"id":6,"name":"Liam Wu","email":"d9c57089f04f2b2e9cd8abcc2e1088afc708fcf60b842d42ab3dc3d3c153e13e","phone":"********","age":25,"amount":500,"plan":"FREE","high_value":true,"joined_at":"2024-04-10"}
{"id":7,"name":"Nina Simone","email":"cec43edb6a1681336ab87fa21ea576e83450826e3cc05f2ca73128e7fd69745f","phone":"********","age":29,"amount":120,"plan":"PRO","high_value":false,"joined_at":"2024-05-12"}
{"id":8,"name":"Owen King","email":"b82aec285b6e6a36fd26fc7404b07b48af53f7b931a492c4b177d58351adba1f","phone":"********","age":31,"amount":999.99,"plan":"ENTERPRISE","high_value":true,"joined_at":"2024-06-18"}
`
	if string(got) != want {
		t.Errorf("out output =\n%s\nwant\n%s", got, want)
	}

	auditPath := filepath.Join("..", "..", "testdata", "etl-errors_audit.jsonl")
	gotAudit, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("reading audit sink: %v", err)
	}
	if string(gotAudit) != string(got) {
		t.Errorf("audit output =\n%s\nwant byte-identical to out output\n%s", gotAudit, got)
	}

	errPath := filepath.Join("..", "..", "testdata", "etl-errors_errors.jsonl")
	gotErr, err := os.ReadFile(errPath)
	if err != nil {
		t.Fatalf("reading error sink: %v", err)
	}
	wantErr := `{"source":"in","ordinal":1,"offset":3,"reason":"missing email","stage":"check"}
{"source":"in","ordinal":4,"offset":6,"reason":"cannot parse \"N/A\" as double","stage":"csv:amount"}
`
	if string(gotErr) != wantErr {
		t.Errorf("error sink =\n%s\nwant\n%s", gotErr, wantErr)
	}
	if strings.Contains(string(gotErr), "Zoe") || strings.Contains(string(gotErr), "Max") {
		t.Error("errsink must never carry a failed row's raw fields, only its envelope")
	}
}

// TestExampleXLSX exercises xlsx.sift: an xlsx source reading a real
// worksheet (people.xlsx, sheet "People", with a title row above the
// header, so header_row: 2 is load-bearing) through the same
// filter/jsonl-sink shape as adults.sift's csv version. That's
// design/xlsx.md's headline claim: no frontend change to read a
// different format.
func TestExampleXLSX(t *testing.T) {
	got := runExample(t, "xlsx")
	want := `{"name":"Ada","age":42}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

// TestExampleColumns is design/column-aliases.md's own example end to
// end: a header with a space ("Transaction ID") is named via the
// columns kwarg, since no identifier could ever equal it.
func TestExampleColumns(t *testing.T) {
	got := runExample(t, "columns")
	want := `{"txn_id":1001,"txn_date":"2026-01-05","amount":42.5}
{"txn_id":1002,"txn_date":"2026-01-06","amount":17.25}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

// TestExampleDate drives testdata/date.sift through the real CLI
// (design/date.md): started_on's default ISO-8601 format and
// renewed_on's day-first formats entry both parse correctly, and
// filter(.renewed_on >= .started_on) drops Tom, whose renewal predates
// his subscription -- a data-quality catch no string comparison could
// make.
func TestExampleDate(t *testing.T) {
	got := runExample(t, "date")
	want := `{"name":"Ada","started_on":"2026-01-05","renewed_on":"2026-02-10"}
{"name":"Grace","started_on":"2026-01-01","renewed_on":"2026-01-01"}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

// TestExampleDecimal drives testdata/decimal.sift through the real CLI
// (design/decimal.md): a bare double literal (0.08) standing against a
// decimal column adapts to decimal for tax, net's subtraction stays
// exact, and filter(.net > 0) drops Tom, whose discount exceeds his
// price. Row 3's price ("1,100.00", design/decimal-leniency.md) proves a
// comma-thousands-formatted cell parses unconditionally, and its trailing
// zeros ("1100.00", "0.00") still round-trip exactly -- the case a naive
// decimal.Decimal-without-a-wrapper implementation gets wrong (it
// silently renders "1100" and "0" instead).
func TestExampleDecimal(t *testing.T) {
	got := runExample(t, "decimal")
	want := `{"id":1,"price":19.99,"discount":5.00,"net":14.99,"tax":1.5992}
{"id":3,"price":1100.00,"discount":0.00,"net":1100.00,"tax":88.0000}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

// TestExampleDateTime drives testdata/datetime.sift through the real CLI
// (design/datetime.md): clock_in/clock_out are space-separated, so
// formats: names their layout explicitly for both -- reusing date's own
// kwarg, not a second one. filter(.clock_out < .clock_in) flags Tom's
// row, whose clock-out time is chronologically before its clock-in on
// the same calendar date (a data-entry error, forgetting to roll the
// date forward for an overnight shift); Ada's ordinary same-day shift and
// Grace's correctly-dated overnight shift are both excluded. The JSON
// output renders with a "T" separator and no zone suffix (DT-F),
// confirming the naive-only decision.
func TestExampleDateTime(t *testing.T) {
	got := runExample(t, "datetime")
	want := `{"name":"Tom","clock_in":"2026-07-31T21:00:00","clock_out":"2026-07-31T05:00:00"}
`
	if string(got) != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}
