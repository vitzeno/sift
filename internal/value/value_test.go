package value

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTypeString(t *testing.T) {
	tests := []struct {
		name string
		typ  Type
		want string
	}{
		{"plain string", Type{Kind: String}, "string"},
		{"plain int", Type{Kind: Int}, "int"},
		{"plain date", Type{Kind: Date}, "date"},
		{"pii string", Type{Kind: String, PII: true}, "string @pii"},
		{"optional string", Type{Kind: String, Optional: true}, "string?"},
		{"optional pii string", Type{Kind: String, Optional: true, PII: true}, "string? @pii"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.typ.String(); got != tt.want {
				t.Errorf("Type.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSchemaString(t *testing.T) {
	s := Schema{Fields: []Field{
		{Name: "name", Type: Type{Kind: String}},
		{Name: "age", Type: Type{Kind: Int}},
	}}
	want := "{ name: string, age: int }"
	if got := s.String(); got != want {
		t.Errorf("Schema.String() = %q, want %q", got, want)
	}
}

func TestSchemaStringWithPII(t *testing.T) {
	s := Schema{Fields: []Field{
		{Name: "email", Type: Type{Kind: String, PII: true}},
	}}
	want := "{ email: string @pii }"
	if got := s.String(); got != want {
		t.Errorf("Schema.String() = %q, want %q", got, want)
	}
}

func TestSchemaLookup(t *testing.T) {
	s := Schema{Fields: []Field{
		{Name: "name", Type: Type{Kind: String}},
		{Name: "age", Type: Type{Kind: Int}},
	}}

	if f, ok := s.Lookup("age"); !ok || f.Type.Kind != Int {
		t.Errorf("Lookup(%q) = %+v, %v; want age:int, true", "age", f, ok)
	}
	if _, ok := s.Lookup("emial"); ok {
		t.Errorf("Lookup(%q) unexpectedly found a field", "emial")
	}
}

func TestSchemaFirstPII(t *testing.T) {
	clean := Schema{Fields: []Field{
		{Name: "name", Type: Type{Kind: String}},
		{Name: "age", Type: Type{Kind: Int}},
	}}
	if _, ok := clean.FirstPII(); ok {
		t.Error("FirstPII found a PII field in a schema with none")
	}

	tagged := Schema{Fields: []Field{
		{Name: "name", Type: Type{Kind: String}},
		{Name: "email", Type: Type{Kind: String, PII: true}},
		{Name: "ssn", Type: Type{Kind: String, PII: true}},
	}}
	f, ok := tagged.FirstPII()
	if !ok || f.Name != "email" {
		t.Errorf("FirstPII() = %+v, %v; want the first PII field, \"email\"", f, ok)
	}
}

// TestRowFailNilByDefault confirms a healthy Row's zero value carries no
// Failure. design-errors.md §2.1 defines nil as "healthy", and every Row
// literal already in the codebase must keep meaning exactly that.
func TestRowFailNilByDefault(t *testing.T) {
	row := Row{Fields: map[string]any{"name": "Ada"}}
	if row.Fail != nil {
		t.Errorf("Fail = %+v, want nil on a Row literal that never set it", row.Fail)
	}
}

func TestRowFailCarriesReasonAndStage(t *testing.T) {
	row := Row{
		Fields: map[string]any{"email": ""},
		Prov:   Provenance{Source: "in", Ordinal: 3},
		Fail:   &Failure{Reason: "missing email", Stage: "check"},
	}
	if row.Fail.Reason != "missing email" {
		t.Errorf("Fail.Reason = %q, want %q", row.Fail.Reason, "missing email")
	}
	if row.Fail.Stage != "check" {
		t.Errorf("Fail.Stage = %q, want %q", row.Fail.Stage, "check")
	}
	// A Failure never duplicates provenance: it rides on the Row that
	// already carries it (design-errors.md §2.1).
	if row.Prov.Ordinal != 3 {
		t.Errorf("Prov.Ordinal = %d, want 3", row.Prov.Ordinal)
	}
}

// TestAbsentMarshalsAsJSONNull checks Absent's sink-format representation
// (design/optional-fields.md §6: no null at the language level, but a
// sink still needs some way to render "no value").
func TestAbsentMarshalsAsJSONNull(t *testing.T) {
	got, err := json.Marshal(Absent{})
	if err != nil {
		t.Fatalf("Marshal(Absent{}): %v", err)
	}
	if string(got) != "null" {
		t.Errorf("Marshal(Absent{}) = %s, want null", got)
	}
}

// TestDateValueString confirms DateValue's own textual form is
// ISO-8601, independent of whatever format the source cell used
// (design/date.md §3).
func TestDateValueString(t *testing.T) {
	d := DateValue(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))
	if got := d.String(); got != "2026-01-05" {
		t.Errorf("DateValue.String() = %q, want %q", got, "2026-01-05")
	}
}

// TestDateValueMarshalsAsISODateString is DATE-F: the JSON form is a
// plain ISO-8601 string, not time.Time's own RFC 3339 (which would leak
// a "T00:00:00Z" time-of-day component a date-only value never had).
func TestDateValueMarshalsAsISODateString(t *testing.T) {
	d := DateValue(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))
	got, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal(DateValue): %v", err)
	}
	want := `"2026-01-05"`
	if string(got) != want {
		t.Errorf("Marshal(DateValue) = %s, want %s", got, want)
	}
}
