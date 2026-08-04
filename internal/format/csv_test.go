package format

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vitzeno/sift/internal/runtime"
	"github.com/vitzeno/sift/internal/value"
)

func peopleSchema() value.Schema {
	return value.Schema{Fields: []value.Field{
		{Name: "name", Type: value.Type{Kind: value.String}},
		{Name: "age", Type: value.Type{Kind: value.Int}},
	}}
}

func TestCSVSourceTypedParseAndProvenance(t *testing.T) {
	src, err := NewCSVSource(runtime.SourceOptions{
		Name:   "in",
		Path:   "../../examples/people.csv",
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
	_, err := NewCSVSource(runtime.SourceOptions{
		Name: "in",
		Path: "../../examples/people.csv",
		Schema: value.Schema{Fields: []value.Field{
			{Name: "emial", Type: value.Type{Kind: value.String}},
		}},
	})
	if err == nil {
		t.Fatal("expected an error for a schema field not present in the CSV header")
	}
}

// TestCSVSourceMalformedRecordIsInfraFatal is ERR-E's source-level half
// (design-errors.md §2.4): a record the reader can't even tokenize into
// the right number of fields leaves no well-formed row to attach a
// per-row Failure to, so Next reports a clean-looking ok=false and the
// real problem surfaces through Err() — never a panic.
func TestCSVSourceMalformedRecordIsInfraFatal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "malformed.csv")
	// The second row has only one field where the header declares two;
	// encoding/csv's Reader rejects this as a field-count mismatch
	// rather than returning a (short) record.
	writeFile(t, path, "name,age\nOnlyOneField\n")

	src, err := NewCSVSource(runtime.SourceOptions{
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

// TestCSVSourceBadCellIsRowFailure is ERR-D (design-errors.md §7): a
// non-numeric "age" cell must become a row Failure — not a panic — and
// the source must keep working normally afterward: the next row still
// reads, and Ordinal/Offset keep advancing as though nothing went wrong,
// since only that one row is marked, never the stream itself.
func TestCSVSourceBadCellIsRowFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "badcell.csv")
	writeFile(t, path, "name,age\nAda,not-a-number\nTom,15\n")

	src, err := NewCSVSource(runtime.SourceOptions{
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

// optionalPhoneSchema is name/phone where phone is Optional — the
// fixture schema for design/optional-fields.md's OF-A/OF-C/OF-D cases.
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

	src, err := NewCSVSource(runtime.SourceOptions{Name: "in", Path: path, Schema: optionalPhoneSchema()})
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

	src, err := NewCSVSource(runtime.SourceOptions{Name: "in", Path: path, Schema: optionalPhoneSchema()})
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
	_, err := NewCSVSource(runtime.SourceOptions{
		Name: "in",
		Path: "../../examples/people.csv",
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

// TestCSVSourceOptionalUnparseableIsRowFailure is OF-D: optionality
// excuses absence, never malformed presence — a present-but-garbage cell
// in an optional numeric field is still a row Failure.
func TestCSVSourceOptionalUnparseableIsRowFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "people.csv")
	writeFile(t, path, "name,age\nAda,not-a-number\n")

	src, err := NewCSVSource(runtime.SourceOptions{
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
