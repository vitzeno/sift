// This file rounds out design/decimal.md's and design/decimal-leniency.md's
// acceptance lists with the cases testdata/decimal.sift + TestExampleDecimal
// can't show: a bad cell routed as a row failure, the compile error the
// whole literal-context design is built around, and a European-formatted
// cell the thousands-comma guard rule must still reject, driven through
// the real CLI like the date phase's integration tests.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDEC_B_BadCellIsRowFailure is DEC-B: a non-numeric cell in a
// decimal column is a row failure, not a construction error and not a
// panic, routed to the error sink like any other bad cell.
func TestDEC_B_BadCellIsRowFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "orders.csv"), "id,price\n1,19.99\n2,not-a-number\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	errPath := filepath.Join(dir, "errors.jsonl")
	writeFile(t, siftPath, `on error |> errors

source in     = csv("orders.csv", schema: { id: int, price: decimal })
sink   out    = jsonl("out.jsonl")
sink   errors = jsonl("errors.jsonl")

pipeline main { in |> out }
`)

	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading %s: %v", outPath, err)
	}
	want := `{"id":1,"price":19.99}` + "\n"
	if string(got) != want {
		t.Errorf("out.jsonl = %q, want %q", got, want)
	}

	errGot, err := os.ReadFile(errPath)
	if err != nil {
		t.Fatalf("reading %s: %v", errPath, err)
	}
	if !strings.Contains(string(errGot), `cannot parse \"not-a-number\" as decimal`) {
		t.Errorf("errors.jsonl = %q, want it to mention the bad cell", errGot)
	}
}

// TestLENIENT_C_EuropeanFormatFailsLoudly is decimal-leniency.md's
// LENIENT-C, driven through the real CLI: a comma sitting after the
// cell's last '.' is European decimal-point formatting, not US/UK
// thousands grouping, so the guard rule refuses to strip it and the row
// fails loudly instead of silently parsing to the wrong number.
func TestLENIENT_C_EuropeanFormatFailsLoudly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "orders.csv"), "id,amount\n1,\"1.234,56\"\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	errPath := filepath.Join(dir, "errors.jsonl")
	writeFile(t, siftPath, `on error |> errors

source in     = csv("orders.csv", schema: { id: int, amount: decimal })
sink   out    = jsonl("out.jsonl")
sink   errors = jsonl("errors.jsonl")

pipeline main { in |> out }
`)

	if err := runFile(siftPath); err != nil {
		t.Fatalf("runFile error: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading %s: %v", outPath, err)
	}
	if string(got) != "" {
		t.Errorf("out.jsonl = %q, want empty (the row should have failed)", got)
	}

	errGot, err := os.ReadFile(errPath)
	if err != nil {
		t.Fatalf("reading %s: %v", errPath, err)
	}
	if !strings.Contains(string(errGot), `cannot parse \"1.234,56\" as decimal`) {
		t.Errorf("errors.jsonl = %q, want it to mention the unparsed European-formatted cell", errGot)
	}
}

// TestDEC_D_NoCrossColumnPromotionThroughRealCLI is DEC-D, driven
// through the checker exactly the way a user would hit it: a decimal
// column and a double column never mix, even though the literal
// exception lets a bare double literal adapt to decimal in the same
// position.
func TestDEC_D_NoCrossColumnPromotionThroughRealCLI(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "orders.csv"), "price,rate\n19.99,0.08\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `source in = csv("orders.csv", schema: { price: decimal, rate: double })
sink   out = jsonl("out.jsonl")
pipeline main { in |> map({ ...row, tax: .price * .rate }) |> out }
`)

	err := runFile(siftPath)
	if err == nil {
		t.Fatal("runFile succeeded, want a compile error: decimal and double never mix")
	}
	if !strings.Contains(err.Error(), "cannot apply * to decimal and double") {
		t.Errorf("error = %v, want it to mention decimal and double", err)
	}
}
