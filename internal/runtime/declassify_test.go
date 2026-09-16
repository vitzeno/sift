package runtime

import (
	"testing"

	"github.com/vitzeno/sift/internal/value"
)

func TestDeclassifyAppliesFnToNamedColumnsOnly(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{
			Fields: map[string]any{"name": "Ada", "email": "ada@example.com"},
			Prov:   value.Provenance{Source: "in", Ordinal: 0},
		},
	}}
	decl := NewDeclassify(src, "mask", []string{"email"})

	row, ok := decl.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want a row")
	}
	if row.Fields["email"] != "***************" {
		t.Errorf("Fields[email] = %#v, want 15 asterisks", row.Fields["email"])
	}
	if row.Fields["name"] != "Ada" {
		t.Errorf("Fields[name] = %#v, want it untouched", row.Fields["name"])
	}
}

func TestDeclassifyRedactStage(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"notes": "sensitive stuff"}},
	}}
	decl := NewDeclassify(src, "redact", []string{"notes"})

	row, ok := decl.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want a row")
	}
	if row.Fields["notes"] != "[REDACTED]" {
		t.Errorf("Fields[notes] = %#v, want \"[REDACTED]\"", row.Fields["notes"])
	}
}

// TestDeclassifyPassesThroughAbsentUntouched covers the declassify stage
// form: a column that's both Optional and @pii has nothing to
// declassify when its value is absent for a given row, so it must pass
// through as value.Absent
// rather than panicking the type assertion mask/hash/redact needs for a
// real string.
func TestDeclassifyPassesThroughAbsentUntouched(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"name": "Tom", "email": value.Absent{}}},
	}}
	decl := NewDeclassify(src, "mask", []string{"email"})

	row, ok := decl.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want a row")
	}
	if _, absent := row.Fields["email"].(value.Absent); !absent {
		t.Errorf("Fields[email] = %#v, want value.Absent", row.Fields["email"])
	}
}

func TestDeclassifyPassesThroughFailedRowUnevaluated(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fail: &value.Failure{Reason: "bad row", Stage: "check"}},
	}}
	decl := NewDeclassify(src, "mask", []string{"email"})

	row, ok := decl.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want the failed row passed through")
	}
	if row.Fail == nil || row.Fail.Reason != "bad row" {
		t.Errorf("Fail = %+v, want it carried through unchanged", row.Fail)
	}
}
