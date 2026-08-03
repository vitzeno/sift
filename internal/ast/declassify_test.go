package ast

import "testing"

func TestBuiltinStageNamesIncludesDeclassifiers(t *testing.T) {
	for _, name := range []string{"mask", "hash", "redact"} {
		if !BuiltinStageNames[name] {
			t.Errorf("BuiltinStageNames[%q] = false, want true", name)
		}
	}
}

func TestDeclassifyShape(t *testing.T) {
	d := &Declassify{Fn: "mask", Columns: []ColumnRef{{Name: "email"}, {Name: "phone"}}}
	if d.Fn != "mask" {
		t.Errorf("Fn = %q, want mask", d.Fn)
	}
	if len(d.Columns) != 2 || d.Columns[0].Name != "email" || d.Columns[1].Name != "phone" {
		t.Errorf("Columns = %+v, want [email, phone]", d.Columns)
	}
}
