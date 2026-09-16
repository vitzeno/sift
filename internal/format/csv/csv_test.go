package csv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vitzeno/sift/internal/runtime"
	"github.com/vitzeno/sift/internal/value"
)

// newTestSource builds a source and closes it when the test ends. These
// tests drive a csvSource directly rather than through runtime.Run,
// which is what closes a source in a real program, so without this the
// file handle stays open for the whole test binary. On Unix that is
// invisible; on Windows t.TempDir's cleanup cannot delete a file that is
// still open, and the test fails there and only there.
func newTestSource(t *testing.T, opts runtime.SourceOptions) (runtime.Source, error) {
	t.Helper()
	src, err := NewCSVSource(opts)
	if src != nil {
		t.Cleanup(func() { _ = src.Close() })
	}
	return src, err
}

func peopleSchema() value.Schema {
	return value.Schema{Fields: []value.Field{
		{Name: "name", Type: value.Type{Kind: value.String}},
		{Name: "age", Type: value.Type{Kind: value.Int}},
	}}
}

func TestCSVSourceTypedParseAndProvenance(t *testing.T) {
	src, err := newTestSource(t, runtime.SourceOptions{
		Name:   "in",
		Path:   "../../../testdata/people.csv",
		Schema: peopleSchema(),
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
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
	// Header is line 1, so Ada's record starts at line 2.
	wantProv := value.Provenance{Source: "in", Ordinal: 0, Offset: 2}
	if row.Prov != wantProv {
		t.Errorf("Prov = %+v, want %+v", row.Prov, wantProv)
	}

	row2, ok := src.Next()
	if !ok {
		t.Fatal("Next() returned ok=false on second row")
	}
	if row2.Prov.Ordinal != 1 || row2.Prov.Offset != 3 {
		t.Errorf("second row Prov = %+v, want Ordinal=1 Offset=3", row2.Prov)
	}

	if _, ok := src.Next(); ok {
		t.Error("Next() returned ok=true past EOF")
	}
}

func TestCSVSourceMissingSchemaField(t *testing.T) {
	_, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: "../../../testdata/people.csv",
		Schema: value.Schema{Fields: []value.Field{
			{Name: "emial", Type: value.Type{Kind: value.String}},
		}},
	})
	if err == nil {
		t.Fatal("expected an error for a schema field not present in the CSV header")
	}
}

// TestCSVSourceMalformedRecordIsInfraFatal confirms a record the reader
// can't even tokenize into
// the right number of fields leaves no well-formed row to attach a
// per-row Failure to, so Next reports a clean-looking ok=false and the
// real problem surfaces through Err(), never a panic.
func TestCSVSourceMalformedRecordIsInfraFatal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "malformed.csv")
	// The second row has only one field where the header declares two;
	// encoding/csv's Reader rejects this as a field-count mismatch
	// rather than returning a (short) record.
	writeFile(t, path, "name,age\nOnlyOneField\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name:   "in",
		Path:   path,
		Schema: peopleSchema(),
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	if _, ok := src.Next(); ok {
		t.Fatal("Next() returned ok=true, want false on a malformed record")
	}
	if err := src.Err(); err == nil {
		t.Fatal("Err() returned nil, want the underlying reader error")
	}
}

// TestCSVSourceBadCellIsRowFailure confirms a non-numeric "age" cell
// becomes a row Failure, not a panic, and
// the source must keep working normally afterward: the next row still
// reads, and Ordinal/Offset keep advancing as though nothing went wrong,
// since only that one row is marked, never the stream itself.
func TestCSVSourceBadCellIsRowFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "badcell.csv")
	writeFile(t, path, "name,age\nAda,not-a-number\nTom,15\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name:   "in",
		Path:   path,
		Schema: peopleSchema(),
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	row, ok := src.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want the failed row returned normally")
	}
	if row.Fail == nil {
		t.Fatal("Fail is nil, want it set for a cell that doesn't parse as int")
	}
	if row.Fail.Reason != `cannot parse "not-a-number" as int` {
		t.Errorf("Fail.Reason = %q, want %q", row.Fail.Reason, `cannot parse "not-a-number" as int`)
	}
	if row.Fail.Stage != "csv:age" {
		t.Errorf("Fail.Stage = %q, want %q", row.Fail.Stage, "csv:age")
	}
	if row.Prov.Ordinal != 0 || row.Prov.Offset != 2 {
		t.Errorf("Prov = %+v, want Ordinal=0 Offset=2 (still attached, even though the row failed)", row.Prov)
	}
	if err := src.Err(); err != nil {
		t.Errorf("Err() = %v, want nil — a bad cell is a row failure, not infra-fatal", err)
	}

	row2, ok := src.Next()
	if !ok {
		t.Fatal("Next() returned ok=false on the second, healthy row")
	}
	if row2.Fail != nil {
		t.Errorf("Fail = %+v, want nil for Tom's healthy row", row2.Fail)
	}
	if row2.Fields["name"] != "Tom" || row2.Fields["age"] != 15 {
		t.Errorf("Fields = %#v, want name=Tom age=15", row2.Fields)
	}
	if row2.Prov.Ordinal != 1 || row2.Prov.Offset != 3 {
		t.Errorf("second row Prov = %+v, want Ordinal=1 Offset=3", row2.Prov)
	}
}

// optionalPhoneSchema is name/phone where phone is Optional: the
// fixture schema for this package's optional-field tests.
func optionalPhoneSchema() value.Schema {
	return value.Schema{Fields: []value.Field{
		{Name: "name", Type: value.Type{Kind: value.String}},
		{Name: "phone", Type: value.Type{Kind: value.String, Optional: true}},
	}}
}

// TestCSVSourceOptionalAbsentFromEmptyCell is OF-A: a blank cell against
// an Optional field reads as value.Absent, not a row Failure.
func TestCSVSourceOptionalAbsentFromEmptyCell(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "people.csv")
	writeFile(t, path, "name,phone\nAda,555-1234\nTom,\n")

	src, err := newTestSource(t, runtime.SourceOptions{Name: "in", Path: path, Schema: optionalPhoneSchema()})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
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

// TestCSVSourceOptionalAbsentFromMissingColumn is OF-C's mirror image: an
// Optional column missing from the header entirely is not a construction
// error, and every row reads that field as Absent.
func TestCSVSourceOptionalAbsentFromMissingColumn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "people.csv")
	writeFile(t, path, "name\nAda\n")

	src, err := newTestSource(t, runtime.SourceOptions{Name: "in", Path: path, Schema: optionalPhoneSchema()})
	if err != nil {
		t.Fatalf("NewCSVSource: %v, want no error for a missing Optional column", err)
	}

	row, ok := src.Next()
	if !ok || row.Fail != nil {
		t.Fatalf("row = %+v, ok=%v, want a healthy row", row, ok)
	}
	if _, absent := row.Fields["phone"].(value.Absent); !absent {
		t.Errorf("Fields[phone] = %#v, want value.Absent", row.Fields["phone"])
	}
}

// TestCSVSourceRequiredColumnMissingIsStructuralError is OF-C: a required
// column absent from the header names the column and the real header.
func TestCSVSourceRequiredColumnMissingIsStructuralError(t *testing.T) {
	_, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: "../../../testdata/people.csv",
		Schema: value.Schema{Fields: []value.Field{
			{Name: "emial", Type: value.Type{Kind: value.String}},
		}},
	})
	if err == nil {
		t.Fatal("expected an error for a required schema field not present in the CSV header")
	}
	if got := err.Error(); !strings.Contains(got, `required column "emial" not found`) {
		t.Errorf("error = %q, want it to name the missing required column", got)
	}
}

// TestCSVSourceColumnAlias confirms a header with a space (never a valid
// identifier) is named via the columns kwarg instead of the field's own
// identifier text.
func TestCSVSourceColumnAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transactions.csv")
	// amount has no columns entry: it resolves by the bare-identifier
	// fallback, which is exact and case-sensitive, so the header must
	// already read "amount", not "Amount".
	writeFile(t, path, "Transaction ID,Date,amount\n1001,2026-01-05,42.50\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "txn_id", Type: value.Type{Kind: value.Int}},
			{Name: "txn_date", Type: value.Type{Kind: value.String}},
			{Name: "amount", Type: value.Type{Kind: value.Double}},
		}},
		Columns: map[string]string{"txn_id": "Transaction ID", "txn_date": "Date"},
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	row, ok := src.Next()
	if !ok || row.Fail != nil {
		t.Fatalf("row = %+v, ok=%v, want a healthy row", row, ok)
	}
	if row.Fields["txn_id"] != 1001 || row.Fields["txn_date"] != "2026-01-05" || row.Fields["amount"] != 42.5 {
		t.Errorf("Fields = %#v, want txn_id=1001 txn_date=2026-01-05 amount=42.5", row.Fields)
	}
}

// TestCSVSourceColumnAliasRequiredMissingIsStructuralError is A-3: no
// alias and no matching identifier for a required field is still a
// construction-time error, naming the field and the real header.
func TestCSVSourceColumnAliasRequiredMissingIsStructuralError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transactions.csv")
	writeFile(t, path, "Transaction ID,Amount\n1001,42.50\n")

	_, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "txn_id", Type: value.Type{Kind: value.Int}},
			{Name: "txn_date", Type: value.Type{Kind: value.String}},
		}},
		Columns: map[string]string{"txn_id": "Transaction ID", "txn_date": "Date"},
	})
	if err == nil {
		t.Fatal("expected an error: no \"Date\" column exists and txn_date is required")
	}
	if got := err.Error(); !strings.Contains(got, `required column "txn_date" not found`) || !strings.Contains(got, "Transaction ID, Amount") {
		t.Errorf("error = %q, want it to name txn_date and the real header", got)
	}
}

// TestCSVSourceColumnAliasOptionalMissingIsAbsent confirms an Optional
// field with an alias whose header isn't in the file at all resolves to
// absent, not a construction error.
func TestCSVSourceColumnAliasOptionalMissingIsAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transactions.csv")
	writeFile(t, path, "Transaction ID,Amount\n1001,42.50\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "txn_id", Type: value.Type{Kind: value.Int}},
			{Name: "txn_date", Type: value.Type{Kind: value.String, Optional: true}},
		}},
		Columns: map[string]string{"txn_id": "Transaction ID", "txn_date": "Date"},
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v, want no error since txn_date is Optional", err)
	}

	row, ok := src.Next()
	if !ok || row.Fail != nil {
		t.Fatalf("row = %+v, ok=%v, want a healthy row", row, ok)
	}
	if _, absent := row.Fields["txn_date"].(value.Absent); !absent {
		t.Errorf("Fields[txn_date] = %#v, want value.Absent", row.Fields["txn_date"])
	}
}

// TestCSVSourceOptionalUnparseableIsRowFailure is OF-D: optionality
// excuses absence, never malformed presence. A present-but-garbage cell
// in an optional numeric field is still a row Failure.
func TestCSVSourceOptionalUnparseableIsRowFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "people.csv")
	writeFile(t, path, "name,age\nAda,not-a-number\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "name", Type: value.Type{Kind: value.String}},
			{Name: "age", Type: value.Type{Kind: value.Int, Optional: true}},
		}},
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	row, ok := src.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want the failed row returned normally")
	}
	if row.Fail == nil {
		t.Fatal("Fail is nil, want a coercion failure for garbage in an optional field")
	}
}

// TestCSVSourceDateDefaultFormat is DATE-A: a date column with no
// formats entry parses against the ISO-8601 default.
func TestCSVSourceDateDefaultFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "signups.csv")
	writeFile(t, path, "name,signup_date\nAda,2026-01-05\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "name", Type: value.Type{Kind: value.String}},
			{Name: "signup_date", Type: value.Type{Kind: value.Date}},
		}},
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	row, ok := src.Next()
	if !ok || row.Fail != nil {
		t.Fatalf("row = %+v, ok=%v, want a healthy row", row, ok)
	}
	got, isDate := row.Fields["signup_date"].(value.DateValue)
	if !isDate {
		t.Fatalf("Fields[signup_date] = %#v, want a value.DateValue", row.Fields["signup_date"])
	}
	if got.String() != "2026-01-05" {
		t.Errorf("signup_date = %s, want 2026-01-05", got.String())
	}
}

// TestCSVSourceDateExplicitFormat is DATE-B: a formats entry drives
// parsing instead of the ISO default, proving day/month order is
// actually read from the kwarg.
func TestCSVSourceDateExplicitFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "uk_export.csv")
	writeFile(t, path, "name,dob\nAda,05/01/2026\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "name", Type: value.Type{Kind: value.String}},
			{Name: "dob", Type: value.Type{Kind: value.Date}},
		}},
		DateFormats: map[string]string{"dob": "02/01/2006"},
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
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

// TestCSVSourceDateTwoFieldsTwoFormats is DATE-B2: two date fields on one
// source, each with its own formats entry, resolve independently on the
// same row.
func TestCSVSourceDateTwoFieldsTwoFormats(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "uk_export.csv")
	writeFile(t, path, "dob,last_login\n05/01/2026,2026-02-10T09:30:00Z\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "dob", Type: value.Type{Kind: value.Date}},
			{Name: "last_login", Type: value.Type{Kind: value.Date}},
		}},
		DateFormats: map[string]string{
			"dob":        "02/01/2006",
			"last_login": "2006-01-02T15:04:05Z",
		},
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	row, ok := src.Next()
	if !ok || row.Fail != nil {
		t.Fatalf("row = %+v, ok=%v, want a healthy row", row, ok)
	}
	if got := row.Fields["dob"].(value.DateValue).String(); got != "2026-01-05" {
		t.Errorf("dob = %s, want 2026-01-05", got)
	}
	if got := row.Fields["last_login"].(value.DateValue).String(); got != "2026-02-10" {
		t.Errorf("last_login = %s, want 2026-02-10", got)
	}
}

// TestCSVSourceDateBadCellIsRowFailure is DATE-C: a cell that doesn't
// match the declared format is a row failure, same shape as a bad
// int/double cell, not a panic.
func TestCSVSourceDateBadCellIsRowFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "signups.csv")
	writeFile(t, path, "name,signup_date\nAda,2026-02-30\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "name", Type: value.Type{Kind: value.String}},
			{Name: "signup_date", Type: value.Type{Kind: value.Date}},
		}},
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	row, ok := src.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want the failed row returned normally")
	}
	if row.Fail == nil {
		t.Fatal("Fail is nil, want a coercion failure for an impossible calendar date")
	}
}

// TestCSVSourceDateTimeDefaultFormat is DT-A: a datetime column with no
// formats entry reads correctly against the ISO-8601-with-time default,
// distinct from Date's own ISO-8601-no-time default.
func TestCSVSourceDateTimeDefaultFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.csv")
	writeFile(t, path, "name,occurred_at\nAda,2026-07-31T04:10:25\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "name", Type: value.Type{Kind: value.String}},
			{Name: "occurred_at", Type: value.Type{Kind: value.DateTime}},
		}},
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	row, ok := src.Next()
	if !ok || row.Fail != nil {
		t.Fatalf("row = %+v, ok=%v, want a healthy row", row, ok)
	}
	got, isDateTime := row.Fields["occurred_at"].(value.DateTimeValue)
	if !isDateTime {
		t.Fatalf("Fields[occurred_at] = %#v, want a value.DateTimeValue", row.Fields["occurred_at"])
	}
	if got.String() != "2026-07-31T04:10:25" {
		t.Errorf("occurred_at = %s, want 2026-07-31T04:10:25", got.String())
	}
}

// TestCSVSourceDateTimeExplicitFormat is DT-B: formats: drives parsing
// against the real Tide file's own space-separated shape, not the
// ISO-8601-with-T default.
func TestCSVSourceDateTimeExplicitFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tide.csv")
	writeFile(t, path, "transaction_id,date\nTX1,2026-07-31 04:10:25\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "transaction_id", Type: value.Type{Kind: value.String}},
			{Name: "date", Type: value.Type{Kind: value.DateTime}},
		}},
		DateFormats: map[string]string{"date": "2006-01-02 15:04:05"},
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	row, ok := src.Next()
	if !ok || row.Fail != nil {
		t.Fatalf("row = %+v, ok=%v, want a healthy row", row, ok)
	}
	got := row.Fields["date"].(value.DateTimeValue)
	if got.String() != "2026-07-31T04:10:25" {
		t.Errorf("date = %s, want 2026-07-31T04:10:25", got.String())
	}
}

// TestCSVSourceDateAndDateTimeTwoFieldsIndependentDefaults proves a date
// field and a datetime field on the same source, both with no formats
// entry, resolve against their own Kind-appropriate default independently
// on the same row.
func TestCSVSourceDateAndDateTimeTwoFieldsIndependentDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.csv")
	writeFile(t, path, "signup_date,last_login\n2026-01-05,2026-02-10T09:30:00\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "signup_date", Type: value.Type{Kind: value.Date}},
			{Name: "last_login", Type: value.Type{Kind: value.DateTime}},
		}},
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	row, ok := src.Next()
	if !ok || row.Fail != nil {
		t.Fatalf("row = %+v, ok=%v, want a healthy row", row, ok)
	}
	if got := row.Fields["signup_date"].(value.DateValue).String(); got != "2026-01-05" {
		t.Errorf("signup_date = %s, want 2026-01-05", got)
	}
	if got := row.Fields["last_login"].(value.DateTimeValue).String(); got != "2026-02-10T09:30:00" {
		t.Errorf("last_login = %s, want 2026-02-10T09:30:00", got)
	}
}

// TestCSVSourceDateTimeBadCellIsRowFailure is DT-C: a cell that matches
// the format but names an impossible time component is a row failure,
// same shape as a bad date/decimal cell, not a panic.
func TestCSVSourceDateTimeBadCellIsRowFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.csv")
	writeFile(t, path, "name,occurred_at\nAda,2026-07-31T25:10:25\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "name", Type: value.Type{Kind: value.String}},
			{Name: "occurred_at", Type: value.Type{Kind: value.DateTime}},
		}},
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	row, ok := src.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want the failed row returned normally")
	}
	if row.Fail == nil {
		t.Fatal("Fail is nil, want a coercion failure for an impossible hour")
	}
}

// TestCSVSourceDecimal is DEC-A's csv half: parses exactly, with
// trailing zeros preserved ("5.00" stays "5.00", not "5" --
// value.DecimalValue's whole reason for existing).
func TestCSVSourceDecimal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orders.csv")
	writeFile(t, path, "id,price\n1,19.99\n2,5.00\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "id", Type: value.Type{Kind: value.Int}},
			{Name: "price", Type: value.Type{Kind: value.Decimal}},
		}},
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	row1, ok := src.Next()
	if !ok || row1.Fail != nil {
		t.Fatalf("row = %+v, ok=%v, want a healthy row", row1, ok)
	}
	if got := row1.Fields["price"].(value.DecimalValue).String(); got != "19.99" {
		t.Errorf("row1 price = %s, want 19.99", got)
	}

	row2, ok := src.Next()
	if !ok || row2.Fail != nil {
		t.Fatalf("row = %+v, ok=%v, want a healthy row", row2, ok)
	}
	if got := row2.Fields["price"].(value.DecimalValue).String(); got != "5.00" {
		t.Errorf("row2 price = %s, want 5.00 (trailing zeros preserved)", got)
	}
}

// TestCSVSourceDecimalThousandsSeparator confirms a comma-thousands-
// formatted cell parses through csv unconditionally, no kwarg needed.
func TestCSVSourceDecimalThousandsSeparator(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orders.csv")
	writeFile(t, path, "id,price\n1,\"2,100.00\"\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "id", Type: value.Type{Kind: value.Int}},
			{Name: "price", Type: value.Type{Kind: value.Decimal}},
		}},
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	row, ok := src.Next()
	if !ok || row.Fail != nil {
		t.Fatalf("row = %+v, ok=%v, want a healthy row", row, ok)
	}
	if got := row.Fields["price"].(value.DecimalValue).String(); got != "2100.00" {
		t.Errorf("price = %s, want 2100.00", got)
	}
}

// TestCSVSourceDecimalBadCellIsRowFailure is DEC-B: a non-numeric cell
// is a row failure, same shape as a bad int/double cell, not a panic.
func TestCSVSourceDecimalBadCellIsRowFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orders.csv")
	writeFile(t, path, "id,price\n1,not-a-number\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name: "in",
		Path: path,
		Schema: value.Schema{Fields: []value.Field{
			{Name: "id", Type: value.Type{Kind: value.Int}},
			{Name: "price", Type: value.Type{Kind: value.Decimal}},
		}},
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	row, ok := src.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want the failed row returned normally")
	}
	if row.Fail == nil {
		t.Fatal("Fail is nil, want a coercion failure for a non-numeric decimal cell")
	}
}

// deidentifySchema is name/email where email is @deidentify, shared by
// this package's deidentify tests.
func deidentifySchema() value.Schema {
	return value.Schema{Fields: []value.Field{
		{Name: "name", Type: value.Type{Kind: value.String}},
		{Name: "email", Type: value.Type{Kind: value.Deidentified, Inner: &value.Type{Kind: value.String}}},
	}}
}

// TestCSVSourceDeidentify confirms csv gets @deidentify for free from
// Coerce's own shared parse path: encrypted output is opaque base64,
// never the plaintext cell.
func TestCSVSourceDeidentify(t *testing.T) {
	t.Setenv("SIFT_TEST_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	dir := t.TempDir()
	path := filepath.Join(dir, "people.csv")
	writeFile(t, path, "name,email\nAda,ada@example.com\n")

	src, err := newTestSource(t, runtime.SourceOptions{
		Name:                "in",
		Path:                path,
		Schema:              deidentifySchema(),
		DeidentifyKeyEnvVar: "SIFT_TEST_KEY",
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	row, ok := src.Next()
	if !ok || row.Fail != nil {
		t.Fatalf("row = %+v, ok=%v, want a healthy row", row, ok)
	}
	got := row.Fields["email"].(string)
	if got == "ada@example.com" {
		t.Error("email field is the plaintext cell, want ciphertext")
	}
}

// TestCSVSourceDeidentifyMissingKeyIsConstructionError confirms a schema
// with a @deidentify field but no key kwarg fails NewCSVSource itself,
// before any row is read.
func TestCSVSourceDeidentifyMissingKeyIsConstructionError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "people.csv")
	writeFile(t, path, "name,email\nAda,ada@example.com\n")

	_, err := newTestSource(t, runtime.SourceOptions{
		Name:   "in",
		Path:   path,
		Schema: deidentifySchema(),
	})
	if err == nil {
		t.Fatal("NewCSVSource succeeded, want a construction error")
	}
	if !strings.Contains(err.Error(), `schema declares @deidentify field "email" but no key: kwarg was given`) {
		t.Errorf("error = %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
