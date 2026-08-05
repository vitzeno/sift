package format

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/vitzeno/sift/internal/runtime"
	"github.com/vitzeno/sift/internal/value"
)

// writeXLSXFixture builds a single-sheet workbook from rows (each a
// row of raw cell strings, in order) and saves it under t.TempDir().
// Generating fixtures with excelize itself, rather than checking in
// binary .xlsx files, keeps every fixture's shape readable as the Go
// code that built it (design/xlsx.md §5).
func writeXLSXFixture(t *testing.T, sheet string, rows [][]string) string {
	t.Helper()
	f := excelize.NewFile()
	if err := f.SetSheetName(f.GetSheetName(0), sheet); err != nil {
		t.Fatalf("SetSheetName: %v", err)
	}
	writeRows(t, f, sheet, rows)
	path := filepath.Join(t.TempDir(), "fixture.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

// writeXLSXWorkbook builds a multi-sheet workbook, first sheet named
// sheets[0], in order. Used by tests that need to assert against a
// real set of sheet names (e.g. a missing-sheet error listing them).
func writeXLSXWorkbook(t *testing.T, sheets []string, rowsBySheet map[string][][]string) string {
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
		writeRows(t, f, name, rowsBySheet[name])
	}
	path := filepath.Join(t.TempDir(), "fixture.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

func writeRows(t *testing.T, f *excelize.File, sheet string, rows [][]string) {
	t.Helper()
	for r, row := range rows {
		for c, val := range row {
			if val == "" {
				continue // leave truly empty cells unwritten, not "" strings
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

func TestXLSXSourceTypedParseAndProvenance(t *testing.T) {
	path := writeXLSXFixture(t, "Sheet1", [][]string{
		{"name", "age"},
		{"Ada", "42"},
		{"Tom", "15"},
	})
	src, err := NewXLSXSource(runtime.SourceOptions{Name: "in", Path: path, Schema: peopleSchema()})
	if err != nil {
		t.Fatalf("NewXLSXSource: %v", err)
	}

	row, ok := src.Next()
	if !ok {
		t.Fatal("Next() returned ok=false on first row")
	}
	if name, ok := row.Fields["name"].(string); !ok || name != "Ada" {
		t.Errorf("Fields[name] = %#v, want string \"Ada\"", row.Fields["name"])
	}
	if age, ok := row.Fields["age"].(int); !ok || age != 42 {
		t.Errorf("Fields[age] = %#v, want int 42", row.Fields["age"])
	}
	wantProv := value.Provenance{Source: "in", Ordinal: 0, Offset: 2}
	if row.Prov != wantProv {
		t.Errorf("Prov = %+v, want %+v", row.Prov, wantProv)
	}

	row2, ok := src.Next()
	if !ok {
		t.Fatal("Next() returned ok=false on second row")
	}
	if row2.Prov.Ordinal != 1 || row2.Prov.Offset != 3 {
		t.Errorf("second row Prov = %+v, want Ordinal:1 Offset:3", row2.Prov)
	}

	if _, ok := src.Next(); ok {
		t.Error("Next() = ok, want false at end of sheet")
	}
	if err := src.Err(); err != nil {
		t.Errorf("Err() = %v, want nil after clean EOF", err)
	}
}

// TestXLSXSourceColumnOrderIgnoresSheetOrder: extra/reordered columns
// in the sheet don't matter. Header binding matches by name
// (design/xlsx.md §2.1).
func TestXLSXSourceColumnOrderIgnoresSheetOrder(t *testing.T) {
	path := writeXLSXFixture(t, "Sheet1", [][]string{
		{"age", "extra", "name"},
		{"42", "unused", "Ada"},
	})
	src, err := NewXLSXSource(runtime.SourceOptions{Name: "in", Path: path, Schema: peopleSchema()})
	if err != nil {
		t.Fatalf("NewXLSXSource: %v", err)
	}
	row, ok := src.Next()
	if !ok {
		t.Fatal("Next() returned ok=false")
	}
	if row.Fields["name"] != "Ada" || row.Fields["age"] != 42 {
		t.Errorf("Fields = %+v, want name=Ada age=42", row.Fields)
	}
}

// TestXLSXSourceHeaderRowAndBlankRows covers XLSX-E: a title row, a
// blank row, the header on row 3, and an extra unmapped column. It
// reads correctly and skips the blank row without ending the stream.
func TestXLSXSourceHeaderRowAndBlankRows(t *testing.T) {
	path := writeXLSXFixture(t, "Data", [][]string{
		{"Q1 Export"},
		{},
		{"name", "age", "notes"},
		{"Ada", "42", "vip"},
		{},
		{"Tom", "15", ""},
	})
	src, err := NewXLSXSource(runtime.SourceOptions{
		Name: "in", Path: path, Schema: peopleSchema(),
		Opts: map[string]any{"header_row": int64(3)},
	})
	if err != nil {
		t.Fatalf("NewXLSXSource: %v", err)
	}

	row, ok := src.Next()
	if !ok || row.Fields["name"] != "Ada" {
		t.Fatalf("first row = %+v, ok=%v, want Ada", row, ok)
	}
	if row.Prov.Offset != 4 {
		t.Errorf("Offset = %d, want 4 (row 3 header + 1)", row.Prov.Offset)
	}

	row2, ok := src.Next()
	if !ok || row2.Fields["name"] != "Tom" {
		t.Fatalf("second row = %+v, ok=%v, want Tom (blank row 5 skipped)", row2, ok)
	}
	if row2.Prov.Ordinal != 1 || row2.Prov.Offset != 6 {
		t.Errorf("Tom Prov = %+v, want Ordinal:1 Offset:6", row2.Prov)
	}

	if _, ok := src.Next(); ok {
		t.Error("Next() = ok, want false at end of sheet")
	}
}

func TestXLSXSourceBadCellIsRowFailure(t *testing.T) {
	path := writeXLSXFixture(t, "Sheet1", [][]string{
		{"name", "age"},
		{"Ada", "not-a-number"},
	})
	src, err := NewXLSXSource(runtime.SourceOptions{Name: "in", Path: path, Schema: peopleSchema()})
	if err != nil {
		t.Fatalf("NewXLSXSource: %v", err)
	}
	row, ok := src.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want a failed row")
	}
	if row.Fail == nil {
		t.Fatal("Fail = nil, want a coercion failure")
	}
	if row.Fail.Stage != "xlsx:age" {
		t.Errorf("Fail.Stage = %q, want xlsx:age", row.Fail.Stage)
	}
}

func TestXLSXSourceMissingSheet(t *testing.T) {
	path := writeXLSXWorkbook(t, []string{"Summary", "Data", "Notes"}, map[string][][]string{
		"Data": {{"name", "age"}, {"Ada", "42"}},
	})
	_, err := NewXLSXSource(runtime.SourceOptions{
		Name: "in", Path: path, Schema: peopleSchema(),
		Opts: map[string]any{"sheet": "Q1 Data"},
	})
	if err == nil {
		t.Fatal("NewXLSXSource error = nil, want a missing-sheet error")
	}
	const want = `source "in": sheet "Q1 Data" not found in`
	if got := err.Error(); len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("error = %q, want prefix %q", got, want)
	}
	if got := err.Error(); !strings.Contains(got, "Summary, Data, Notes") {
		t.Errorf("error = %q, want it to list the available sheets", got)
	}
}

func TestXLSXSourceSheetSelection(t *testing.T) {
	path := writeXLSXWorkbook(t, []string{"Summary", "Data"}, map[string][][]string{
		"Summary": {{"junk"}},
		"Data":    {{"name", "age"}, {"Ada", "42"}},
	})
	src, err := NewXLSXSource(runtime.SourceOptions{
		Name: "in", Path: path, Schema: peopleSchema(),
		Opts: map[string]any{"sheet": "Data"},
	})
	if err != nil {
		t.Fatalf("NewXLSXSource: %v", err)
	}
	row, ok := src.Next()
	if !ok || row.Fields["name"] != "Ada" {
		t.Fatalf("row = %+v, ok=%v, want Ada from the Data sheet", row, ok)
	}
}

func TestXLSXSourceMissingColumn(t *testing.T) {
	path := writeXLSXFixture(t, "Sheet1", [][]string{
		{"name", "years"},
		{"Ada", "42"},
	})
	_, err := NewXLSXSource(runtime.SourceOptions{Name: "in", Path: path, Schema: peopleSchema()})
	if err == nil {
		t.Fatal("NewXLSXSource error = nil, want a missing-column error")
	}
	if got := err.Error(); !strings.Contains(got, `required column "age" not found`) || !strings.Contains(got, "name, years") {
		t.Errorf("error = %q, want it to name the missing required column and list the real header", got)
	}
}

// TestXLSXSourceColumnAliasWithOffsetHeaderRow is design/column-aliases.md's
// acceptance case B: a spaced header resolves via an alias, proving
// aliasing composes with an offset header_row and is format-agnostic
// (it resolves identically to the csv case).
func TestXLSXSourceColumnAliasWithOffsetHeaderRow(t *testing.T) {
	path := writeXLSXFixture(t, "Sheet1", [][]string{
		{"Q1 Export"},
		{},
		{"Transaction ID", "Date"},
		{"1001", "2026-01-05"},
	})
	src, err := NewXLSXSource(runtime.SourceOptions{
		Name: "in", Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "txn_id", Type: value.Type{Kind: value.Int}},
			{Name: "txn_date", Type: value.Type{Kind: value.String}},
		}},
		Columns: map[string]string{"txn_id": "Transaction ID", "txn_date": "Date"},
		Opts:    map[string]any{"header_row": int64(3)},
	})
	if err != nil {
		t.Fatalf("NewXLSXSource: %v", err)
	}

	row, ok := src.Next()
	if !ok || row.Fail != nil {
		t.Fatalf("row = %+v, ok=%v, want a healthy row", row, ok)
	}
	if row.Fields["txn_id"] != 1001 || row.Fields["txn_date"] != "2026-01-05" {
		t.Errorf("Fields = %#v, want txn_id=1001 txn_date=2026-01-05", row.Fields)
	}
}

// TestXLSXSourceOptionalAbsentFromEmptyCell is OF-A's xlsx half: a blank
// cell against an Optional field reads as value.Absent, not a Failure.
func TestXLSXSourceOptionalAbsentFromEmptyCell(t *testing.T) {
	path := writeXLSXFixture(t, "Sheet1", [][]string{
		{"name", "phone"},
		{"Ada", "555-1234"},
		{"Tom", ""},
	})
	src, err := NewXLSXSource(runtime.SourceOptions{Name: "in", Path: path, Schema: optionalPhoneSchema()})
	if err != nil {
		t.Fatalf("NewXLSXSource: %v", err)
	}

	row, ok := src.Next()
	if !ok || row.Fail != nil {
		t.Fatalf("first row = %+v, ok=%v, want a healthy row", row, ok)
	}
	if row.Fields["phone"] != "555-1234" {
		t.Errorf("Fields[phone] = %#v, want \"555-1234\"", row.Fields["phone"])
	}

	row2, ok := src.Next()
	if !ok || row2.Fail != nil {
		t.Fatalf("second row = %+v, ok=%v, want a healthy row (absence isn't a failure)", row2, ok)
	}
	if _, absent := row2.Fields["phone"].(value.Absent); !absent {
		t.Errorf("Fields[phone] = %#v, want value.Absent", row2.Fields["phone"])
	}
}

// TestXLSXSourceOptionalAbsentFromMissingColumn is OF-C's xlsx half: an
// Optional column missing from the header entirely is not a construction
// error, and every row reads that field as Absent.
func TestXLSXSourceOptionalAbsentFromMissingColumn(t *testing.T) {
	path := writeXLSXFixture(t, "Sheet1", [][]string{
		{"name"},
		{"Ada"},
	})
	src, err := NewXLSXSource(runtime.SourceOptions{Name: "in", Path: path, Schema: optionalPhoneSchema()})
	if err != nil {
		t.Fatalf("NewXLSXSource: %v, want no error for a missing Optional column", err)
	}

	row, ok := src.Next()
	if !ok || row.Fail != nil {
		t.Fatalf("row = %+v, ok=%v, want a healthy row", row, ok)
	}
	if _, absent := row.Fields["phone"].(value.Absent); !absent {
		t.Errorf("Fields[phone] = %#v, want value.Absent", row.Fields["phone"])
	}
}

func TestXLSXSourceDuplicateHeader(t *testing.T) {
	path := writeXLSXFixture(t, "Sheet1", [][]string{
		{"name", "age", "name"},
		{"Ada", "42", "Ada Again"},
	})
	_, err := NewXLSXSource(runtime.SourceOptions{Name: "in", Path: path, Schema: peopleSchema()})
	if err == nil {
		t.Fatal("NewXLSXSource error = nil, want a duplicate-column error")
	}
	if got := err.Error(); !strings.Contains(got, `duplicate column "name"`) {
		t.Errorf("error = %q, want it to name the duplicate column", got)
	}
}

func TestXLSXSourceHeaderRowBeyondSheet(t *testing.T) {
	path := writeXLSXFixture(t, "Sheet1", [][]string{
		{"name", "age"},
		{"Ada", "42"},
	})
	_, err := NewXLSXSource(runtime.SourceOptions{
		Name: "in", Path: path, Schema: peopleSchema(),
		Opts: map[string]any{"header_row": int64(7)},
	})
	if err == nil {
		t.Fatal("NewXLSXSource error = nil, want a too-few-rows error")
	}
	if got := err.Error(); !strings.Contains(got, "has 2 rows; header_row is 7") {
		t.Errorf("error = %q, want it to report the real row count", got)
	}
}

func TestXLSXSourceEmptyHeaderCellsIgnored(t *testing.T) {
	path := writeXLSXFixture(t, "Sheet1", [][]string{
		{"name", "", "age"},
		{"Ada", "stray", "42"},
	})
	src, err := NewXLSXSource(runtime.SourceOptions{Name: "in", Path: path, Schema: peopleSchema()})
	if err != nil {
		t.Fatalf("NewXLSXSource: %v", err)
	}
	row, ok := src.Next()
	if !ok || row.Fields["name"] != "Ada" || row.Fields["age"] != 42 {
		t.Fatalf("row = %+v, ok=%v, want name=Ada age=42", row, ok)
	}
}

// TestXLSXSourceDate is DATE-G: a date-typed field reads correctly from
// an xlsx fixture with no format-specific code path of its own --
// regression-shaped, proving design/date.md §3's "xlsx needs no special
// handling" claim, since excelize's row iterator already delivers cells
// as strings the same way csv's reader does.
func TestXLSXSourceDate(t *testing.T) {
	path := writeXLSXFixture(t, "Sheet1", [][]string{
		{"name", "dob"},
		{"Ada", "05/01/2026"},
	})
	src, err := NewXLSXSource(runtime.SourceOptions{
		Name: "in", Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "name", Type: value.Type{Kind: value.String}},
			{Name: "dob", Type: value.Type{Kind: value.Date}},
		}},
		DateFormats: map[string]string{"dob": "02/01/2006"},
	})
	if err != nil {
		t.Fatalf("NewXLSXSource: %v", err)
	}

	row, ok := src.Next()
	if !ok || row.Fail != nil {
		t.Fatalf("row = %+v, ok=%v, want a healthy row", row, ok)
	}
	got := row.Fields["dob"].(value.DateValue)
	if got.String() != "2026-01-05" {
		t.Errorf("dob = %s, want 2026-01-05 (5 January, day-first)", got.String())
	}
}

// TestXLSXSourceDecimal is DEC-A's xlsx half: a decimal-typed field
// reads correctly from an xlsx fixture with no format-specific code
// path of its own -- regression-shaped, proving design/decimal.md §3's
// "no source/sink code changes" claim, since excelize's row iterator
// already delivers cells as strings the same way csv's reader does.
// "5.00" is deliberately not "5": trailing zeros must round-trip
// through xlsx exactly like they do through csv (value.DecimalValue's
// fix isn't csv-specific).
func TestXLSXSourceDecimal(t *testing.T) {
	path := writeXLSXFixture(t, "Sheet1", [][]string{
		{"id", "price"},
		{"1", "5.00"},
	})
	src, err := NewXLSXSource(runtime.SourceOptions{
		Name: "in", Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "id", Type: value.Type{Kind: value.Int}},
			{Name: "price", Type: value.Type{Kind: value.Decimal}},
		}},
	})
	if err != nil {
		t.Fatalf("NewXLSXSource: %v", err)
	}

	row, ok := src.Next()
	if !ok || row.Fail != nil {
		t.Fatalf("row = %+v, ok=%v, want a healthy row", row, ok)
	}
	got := row.Fields["price"].(value.DecimalValue)
	if got.String() != "5.00" {
		t.Errorf("price = %s, want 5.00 (trailing zeros preserved)", got.String())
	}
}
