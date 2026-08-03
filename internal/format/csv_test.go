package format

import (
	"os"
	"path/filepath"
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
		Path:   "../../testdata/people.csv",
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
		Path: "../../testdata/people.csv",
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
