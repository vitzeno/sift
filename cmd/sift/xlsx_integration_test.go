// This file maps to design/xlsx.md §5's acceptance list (XLSX-A through
// XLSX-G), driving the xlsx source through runFile like every other
// phase's integration tests. Fixture workbooks are generated with
// excelize at test time, not checked in as binaries, matching
// internal/format/xlsx_test.go's convention: a fixture's shape is
// readable straight from the Go code that builds it.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// xlsxFixture writes a single-sheet workbook under dir and returns its
// path.
func xlsxFixture(t *testing.T, dir, filename, sheet string, rows [][]string) string {
	t.Helper()
	f := excelize.NewFile()
	if err := f.SetSheetName(f.GetSheetName(0), sheet); err != nil {
		t.Fatalf("SetSheetName: %v", err)
	}
	xlsxWriteRows(t, f, sheet, rows)
	path := filepath.Join(dir, filename)
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

// xlsxWorkbook writes a multi-sheet workbook, first sheet named
// sheets[0], under dir and returns its path.
func xlsxWorkbook(t *testing.T, dir, filename string, sheets []string, rowsBySheet map[string][][]string) string {
	t.Helper()
	f := excelize.NewFile()
	if err := f.SetSheetName(f.GetSheetName(0), sheets[0]); err != nil {
		t.Fatalf("SetSheetName: %v", err)
	}
	for _, name := range sheets[1:] {
		if _, err := f.NewSheet(name); err != nil {
			t.Fatalf("NewSheet(%q): %v", name, err)
		}
	}
	for _, name := range sheets {
		xlsxWriteRows(t, f, name, rowsBySheet[name])
	}
	path := filepath.Join(dir, filename)
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

func xlsxWriteRows(t *testing.T, f *excelize.File, sheet string, rows [][]string) {
	t.Helper()
	for r, row := range rows {
		for c, val := range row {
			if val == "" {
				continue
			}
			cell, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				t.Fatalf("CoordinatesToCellName: %v", err)
			}
			if err := f.SetCellStr(sheet, cell, val); err != nil {
				t.Fatalf("SetCellStr(%s): %v", cell, err)
			}
		}
	}
}

// TestXLSX_A_ParityWithCSV: the same adults program with only the
// source format swapped produces byte-identical output. The headline
// proof that the xlsx connector needs nothing special from the
// lexer/parser/checker/executor.
func TestXLSX_A_ParityWithCSV(t *testing.T) {
	dir := t.TempDir()
	rows := [][]string{
		{"name", "age"},
		{"Ada", "42"},
		{"Tom", "15"},
		{"Grace", "30"},
	}

	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\nTom,15\nGrace,30\n")
	xlsxFixture(t, dir, "people.xlsx", "Sheet1", rows)

	const schema = `{ name: string, age: int }`
	writeFile(t, filepath.Join(dir, "csv.sift"), `source in = csv("people.csv", schema: `+schema+`)
sink out = jsonl("csv_out.jsonl")
pipeline main { in |> filter(.age >= 18) |> out }
`)
	writeFile(t, filepath.Join(dir, "xlsx.sift"), `source in = xlsx("people.xlsx", schema: `+schema+`)
sink out = jsonl("xlsx_out.jsonl")
pipeline main { in |> filter(.age >= 18) |> out }
`)

	if err := runFile(filepath.Join(dir, "csv.sift"), false); err != nil {
		t.Fatalf("runFile(csv, false): %v", err)
	}
	if err := runFile(filepath.Join(dir, "xlsx.sift"), false); err != nil {
		t.Fatalf("runFile(xlsx, false): %v", err)
	}

	csvOut, err := os.ReadFile(filepath.Join(dir, "csv_out.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	xlsxOut, err := os.ReadFile(filepath.Join(dir, "xlsx_out.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(csvOut) != string(xlsxOut) {
		t.Errorf("xlsx output = %s, want byte-identical to csv output %s", xlsxOut, csvOut)
	}
}

// TestXLSX_B_SheetAndHeaderRowKwargsTakeEffect: both keyword args parse
// through the generic kwarg handling (no xlsx-specific grammar) and the
// constructor honors them, reading the named sheet from the declared
// header row.
func TestXLSX_B_SheetAndHeaderRowKwargsTakeEffect(t *testing.T) {
	dir := t.TempDir()
	xlsxWorkbook(t, dir, "people.xlsx", []string{"Cover", "Q1"}, map[string][][]string{
		"Cover": {{"ignore me"}},
		"Q1": {
			{"Q1 export"},
			{"name", "age"},
			{"Ada", "42"},
		},
	})
	writeFile(t, filepath.Join(dir, "prog.sift"), `source in = xlsx("people.xlsx", sheet: "Q1", header_row: 2, schema: { name: string, age: int })
sink out = jsonl("out.jsonl")
pipeline main { in |> out }
`)
	if err := runFile(filepath.Join(dir, "prog.sift"), false); err != nil {
		t.Fatalf("runFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "out.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\"name\":\"Ada\",\"age\":42}\n"; string(got) != want {
		t.Errorf("output = %s, want %s", got, want)
	}
}

// TestXLSX_C_BadCellGovernedByErrorPolicy: a non-numeric age cell is a
// row failure, not a panic, and follows the active error policy. Same
// contract as ERR-D/errors-E3, now through the xlsx source's coercion
// path.
func TestXLSX_C_BadCellGovernedByErrorPolicy(t *testing.T) {
	dir := t.TempDir()
	xlsxFixture(t, dir, "people.xlsx", "Sheet1", [][]string{
		{"name", "age"},
		{"Ada", "42"},
		{"Tom", "not-a-number"},
		{"Grace", "30"},
	})

	t.Run("skip", func(t *testing.T) {
		writeFile(t, filepath.Join(dir, "skip.sift"), `on error skip
source in = xlsx("people.xlsx", schema: { name: string, age: int })
sink out = jsonl("skip_out.jsonl")
pipeline main { in |> out }
`)
		if err := runFile(filepath.Join(dir, "skip.sift"), false); err != nil {
			t.Fatalf("runFile: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(dir, "skip_out.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		want := "{\"name\":\"Ada\",\"age\":42}\n{\"name\":\"Grace\",\"age\":30}\n"
		if string(got) != want {
			t.Errorf("output = %s, want %s", got, want)
		}
	})

	t.Run("route", func(t *testing.T) {
		writeFile(t, filepath.Join(dir, "route.sift"), `on error |> errs
source in = xlsx("people.xlsx", schema: { name: string, age: int })
sink out = jsonl("route_out.jsonl")
sink errs = jsonl("route_errs.jsonl")
pipeline main { in |> out }
`)
		if err := runFile(filepath.Join(dir, "route.sift"), false); err != nil {
			t.Fatalf("runFile: %v", err)
		}
		errs, err := os.ReadFile(filepath.Join(dir, "route_errs.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(errs), `"stage":"xlsx:age"`) {
			t.Errorf("errs = %s, want a stage:\"xlsx:age\" envelope", errs)
		}
	})
}

// TestXLSX_D_MissingSheetIsInfraFatal: a named sheet that doesn't exist
// fails at construction, before any row flows, with a clear message.
func TestXLSX_D_MissingSheetIsInfraFatal(t *testing.T) {
	dir := t.TempDir()
	xlsxWorkbook(t, dir, "people.xlsx", []string{"Summary", "Data", "Notes"}, map[string][][]string{
		"Data": {{"name", "age"}, {"Ada", "42"}},
	})
	writeFile(t, filepath.Join(dir, "prog.sift"), `source in = xlsx("people.xlsx", sheet: "Q1 Data", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")
pipeline main { in |> out }
`)
	err := runFile(filepath.Join(dir, "prog.sift"), false)
	if err == nil {
		t.Fatal("runFile succeeded, want a missing-sheet error")
	}
	for _, want := range []string{`sheet "Q1 Data" not found`, "Summary, Data, Notes"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to contain %q", err, want)
		}
	}
}

// TestXLSX_E_OffsetHeaderIgnoresExtraColumn: title row, blank row,
// header on row 3, and an extra unmapped column. Reads correctly and
// ignores the extra column.
func TestXLSX_E_OffsetHeaderIgnoresExtraColumn(t *testing.T) {
	dir := t.TempDir()
	xlsxFixture(t, dir, "people.xlsx", "Sheet1", [][]string{
		{"People Export"},
		{},
		{"name", "age", "notes"},
		{"Ada", "42", "vip"},
	})
	writeFile(t, filepath.Join(dir, "prog.sift"), `source in = xlsx("people.xlsx", header_row: 3, schema: { name: string, age: int })
sink out = jsonl("out.jsonl")
pipeline main { in |> out }
`)
	if err := runFile(filepath.Join(dir, "prog.sift"), false); err != nil {
		t.Fatalf("runFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "out.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\"name\":\"Ada\",\"age\":42}\n"; string(got) != want {
		t.Errorf("output = %s, want %s (notes column ignored)", got, want)
	}
}

// TestXLSX_F_MissingColumnListsRealHeader: a declared field absent
// from the header fails at construction, and the message lists the
// header's real columns.
func TestXLSX_F_MissingColumnListsRealHeader(t *testing.T) {
	dir := t.TempDir()
	xlsxFixture(t, dir, "people.xlsx", "Sheet1", [][]string{
		{"name", "years"},
		{"Ada", "42"},
	})
	writeFile(t, filepath.Join(dir, "prog.sift"), `source in = xlsx("people.xlsx", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")
pipeline main { in |> out }
`)
	err := runFile(filepath.Join(dir, "prog.sift"), false)
	if err == nil {
		t.Fatal("runFile succeeded, want a missing-column error")
	}
	for _, want := range []string{`column "age" not found`, "name, years"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to contain %q", err, want)
		}
	}
}

// TestXLSX_G_FailFastOrderingLeavesSinkUntouched: sources are
// constructed, and can fail, before any sink is opened. Pre-creating
// the output file and asserting it survives a failed run catches a
// refactor that opens sinks first; every other test here would still
// pass even if that ordering broke.
func TestXLSX_G_FailFastOrderingLeavesSinkUntouched(t *testing.T) {
	dir := t.TempDir()
	xlsxWorkbook(t, dir, "people.xlsx", []string{"Data"}, map[string][][]string{
		"Data": {{"name", "age"}, {"Ada", "42"}},
	})
	writeFile(t, filepath.Join(dir, "prog.sift"), `source in = xlsx("people.xlsx", sheet: "Missing", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")
pipeline main { in |> out }
`)
	outPath := filepath.Join(dir, "out.jsonl")
	const sentinel = "pre-existing output, must not be touched\n"
	writeFile(t, outPath, sentinel)

	if err := runFile(filepath.Join(dir, "prog.sift"), false); err == nil {
		t.Fatal("runFile succeeded, want a missing-sheet error")
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != sentinel {
		t.Errorf("out.jsonl = %q, want it untouched (%q) — a sink was opened before the source construction failure", got, sentinel)
	}
}
