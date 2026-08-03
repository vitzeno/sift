package ast

import "testing"

func TestBuiltinStageNamesIncludesRename(t *testing.T) {
	if !BuiltinStageNames["rename"] {
		t.Error(`BuiltinStageNames["rename"] = false, want true`)
	}
}

func TestRenameShape(t *testing.T) {
	r := &Rename{Pairs: []RenamePair{
		{Old: "email_addr", New: "email"},
		{Old: "dob", New: "birth_date"},
	}}
	if len(r.Pairs) != 2 {
		t.Fatalf("Pairs = %+v, want 2 entries", r.Pairs)
	}
	if r.Pairs[0].Old != "email_addr" || r.Pairs[0].New != "email" {
		t.Errorf("Pairs[0] = %+v, want {Old: email_addr, New: email}", r.Pairs[0])
	}
}
