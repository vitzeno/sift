package ast

import "testing"

func TestBuiltinStageNamesIncludesSelectAndDrop(t *testing.T) {
	for _, name := range []string{"filter", "map", "check", "select", "drop"} {
		if !BuiltinStageNames[name] {
			t.Errorf("BuiltinStageNames[%q] = false, want true", name)
		}
	}
	if BuiltinStageNames["clean"] {
		t.Error(`BuiltinStageNames["clean"] = true, want false (not a built-in name)`)
	}
}

func TestSelectDropShape(t *testing.T) {
	sel := &Select{Columns: []ColumnRef{{Name: "name"}, {Name: "email"}}}
	if len(sel.Columns) != 2 || sel.Columns[0].Name != "name" || sel.Columns[1].Name != "email" {
		t.Errorf("Select.Columns = %+v, want [name, email] in order", sel.Columns)
	}

	drop := &Drop{Columns: []ColumnRef{{Name: "ssn"}}}
	if len(drop.Columns) != 1 || drop.Columns[0].Name != "ssn" {
		t.Errorf("Drop.Columns = %+v, want [ssn]", drop.Columns)
	}
}
