package value

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
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
		{"plain decimal", Type{Kind: Decimal}, "decimal"},
		{"plain datetime", Type{Kind: DateTime}, "datetime"},
		{"pii string", Type{Kind: String, PII: true}, "string @pii"},
		{"optional string", Type{Kind: String, Optional: true}, "string?"},
		{"optional pii string", Type{Kind: String, Optional: true, PII: true}, "string? @pii"},
		{"deidentified string", Type{Kind: Deidentified, Inner: &Type{Kind: String}}, "deidentified<string>"},
		{"deidentified int", Type{Kind: Deidentified, Inner: &Type{Kind: Int}}, "deidentified<int>"},
		{"deidentified optional string", Type{Kind: Deidentified, Inner: &Type{Kind: String, Optional: true}}, "deidentified<string?>"},
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

func TestSchemaFirstDeidentified(t *testing.T) {
	clean := Schema{Fields: []Field{
		{Name: "name", Type: Type{Kind: String}},
	}}
	if _, ok := clean.FirstDeidentified(); ok {
		t.Error("FirstDeidentified found a deidentified field in a schema with none")
	}

	tagged := Schema{Fields: []Field{
		{Name: "name", Type: Type{Kind: String}},
		{Name: "email", Type: Type{Kind: Deidentified, Inner: &Type{Kind: String}}},
	}}
	f, ok := tagged.FirstDeidentified()
	if !ok || f.Name != "email" {
		t.Errorf("FirstDeidentified() = %+v, %v; want the first deidentified field, \"email\"", f, ok)
	}
}

// TestRowFailNilByDefault confirms a healthy Row's zero value carries no
// Failure: nil means healthy, and every Row literal in the codebase must
// keep meaning exactly that.
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
	// already carries it.
	if row.Prov.Ordinal != 3 {
		t.Errorf("Prov.Ordinal = %d, want 3", row.Prov.Ordinal)
	}
}

// TestAbsentMarshalsAsJSONNull checks Absent's sink-format
// representation. Sift has no null at the language level, but a sink
// still needs some way to render "no value".
func TestAbsentMarshalsAsJSONNull(t *testing.T) {
	got, err := json.Marshal(Absent{})
	if err != nil {
		t.Fatalf("Marshal(Absent{}): %v", err)
	}
	if string(got) != "null" {
		t.Errorf("Marshal(Absent{}) = %s, want null", got)
	}
}

// TestDateValueString confirms DateValue's own textual form is ISO-8601,
// independent of whatever format the source cell used.
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

// TestDecimalValueString confirms DecimalValue renders at its own
// remembered scale, unlike decimal.Decimal's own String(), which
// silently strips trailing zeros.
func TestDecimalValueString(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"19.99", "19.99"},
		{"5.00", "5.00"},
		{"0.00", "0.00"},
		{"100", "100"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			d := DecimalValue(decimal.RequireFromString(tt.raw))
			if got := d.String(); got != tt.want {
				t.Errorf("DecimalValue.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDecimalValueMarshalsAsBareNumericLiteral is DEC-G: the JSON form
// is a bare numeric literal (no quotes, matching int/double), not
// decimal.Decimal's own default MarshalJSON (which, without explicitly
// setting the package-level decimal.MarshalJSONWithoutQuotes, renders as
// a quoted string -- confirmed empirically). It also preserves the
// original scale, the same trailing-zero case TestDecimalValueString
// checks for String().
func TestDecimalValueMarshalsAsBareNumericLiteral(t *testing.T) {
	d := DecimalValue(decimal.RequireFromString("5.00"))
	got, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal(DecimalValue): %v", err)
	}
	want := `5.00`
	if string(got) != want {
		t.Errorf("Marshal(DecimalValue) = %s, want %s (bare literal, no quotes)", got, want)
	}
}

// TestDateTimeValueString confirms DateTimeValue's own textual form is
// ISO-8601 with a time component and no zone suffix, independent of
// whatever format the source cell used.
func TestDateTimeValueString(t *testing.T) {
	d := DateTimeValue(time.Date(2026, 7, 31, 4, 10, 25, 0, time.UTC))
	if got := d.String(); got != "2026-07-31T04:10:25" {
		t.Errorf("DateTimeValue.String() = %q, want %q", got, "2026-07-31T04:10:25")
	}
}

// TestDateTimeValueMarshalsAsISOString is DT-F: the JSON form is a plain
// string in DateTimeValue's own layout, not time.Time's own RFC 3339
// (which would imply a "Z" zone this naive value never had).
func TestDateTimeValueMarshalsAsISOString(t *testing.T) {
	d := DateTimeValue(time.Date(2026, 7, 31, 4, 10, 25, 0, time.UTC))
	got, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal(DateTimeValue): %v", err)
	}
	want := `"2026-07-31T04:10:25"`
	if string(got) != want {
		t.Errorf("Marshal(DateTimeValue) = %s, want %s", got, want)
	}
}
