package runtime

import (
	"testing"

	"github.com/vitzeno/sift/internal/value"
)

func TestSelectKeepsOnlyNamedColumnsInOrder(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{
			Fields: map[string]any{"name": "Ada", "age": 42, "email": "ada@example.com"},
			Prov:   value.Provenance{Source: "in", Ordinal: 0},
		},
	}}
	sel := NewSelect(src, []string{"email", "name"})

	row, ok := sel.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want a row")
	}
	if len(row.Fields) != 2 {
		t.Fatalf("Fields = %#v, want exactly 2 entries", row.Fields)
	}
	if row.Fields["email"] != "ada@example.com" || row.Fields["name"] != "Ada" {
		t.Errorf("Fields = %#v, want email and name kept", row.Fields)
	}
	if _, ok := row.Fields["age"]; ok {
		t.Error("Fields still has \"age\", want it dropped by select")
	}
	if row.Prov != (value.Provenance{Source: "in", Ordinal: 0}) {
		t.Errorf("Prov = %+v, want it carried over unchanged", row.Prov)
	}
}

func TestSelectPassesThroughFailedRowUnevaluated(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fail: &value.Failure{Reason: "bad row", Stage: "check"}},
	}}
	sel := NewSelect(src, []string{"name"})

	row, ok := sel.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want the failed row passed through")
	}
	if row.Fail == nil || row.Fail.Reason != "bad row" {
		t.Errorf("Fail = %+v, want it carried through unchanged", row.Fail)
	}
}

func TestDropKeepsEveryOtherColumn(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{
			Fields: map[string]any{"name": "Ada", "age": 42, "ssn": "000-00-0000"},
			Prov:   value.Provenance{Source: "in", Ordinal: 0},
		},
	}}
	drop := NewDrop(src, []string{"ssn"})

	row, ok := drop.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want a row")
	}
	if len(row.Fields) != 2 {
		t.Fatalf("Fields = %#v, want exactly 2 entries", row.Fields)
	}
	if row.Fields["name"] != "Ada" || row.Fields["age"] != 42 {
		t.Errorf("Fields = %#v, want name and age kept", row.Fields)
	}
	if _, ok := row.Fields["ssn"]; ok {
		t.Error("Fields still has \"ssn\", want it dropped")
	}
}

func TestDropPassesThroughFailedRowUnevaluated(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fail: &value.Failure{Reason: "bad row", Stage: "check"}},
	}}
	drop := NewDrop(src, []string{"ssn"})

	row, ok := drop.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want the failed row passed through")
	}
	if row.Fail == nil || row.Fail.Reason != "bad row" {
		t.Errorf("Fail = %+v, want it carried through unchanged", row.Fail)
	}
}
