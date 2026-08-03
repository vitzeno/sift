package runtime

import (
	"testing"

	"github.com/vitzeno/sift/internal/value"
)

func TestRenameRemapsKeysKeepsValues(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{
			Fields: map[string]any{"dob": "1990-01-01", "name": "Ada"},
			Prov:   value.Provenance{Source: "in", Ordinal: 0},
		},
	}}
	ren := NewRename(src, map[string]string{"dob": "birth_date"})

	row, ok := ren.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want a row")
	}
	if len(row.Fields) != 2 {
		t.Fatalf("Fields = %#v, want exactly 2 entries", row.Fields)
	}
	if row.Fields["birth_date"] != "1990-01-01" {
		t.Errorf("Fields[birth_date] = %#v, want \"1990-01-01\"", row.Fields["birth_date"])
	}
	if row.Fields["name"] != "Ada" {
		t.Errorf("Fields[name] = %#v, want \"Ada\" (untouched)", row.Fields["name"])
	}
	if _, ok := row.Fields["dob"]; ok {
		t.Error("Fields still has \"dob\", want it renamed away")
	}
}

func TestRenamePassesThroughFailedRowUnevaluated(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fail: &value.Failure{Reason: "bad row", Stage: "check"}},
	}}
	ren := NewRename(src, map[string]string{"dob": "birth_date"})

	row, ok := ren.Next()
	if !ok {
		t.Fatal("Next() returned ok=false, want the failed row passed through")
	}
	if row.Fail == nil || row.Fail.Reason != "bad row" {
		t.Errorf("Fail = %+v, want it carried through unchanged", row.Fail)
	}
}
