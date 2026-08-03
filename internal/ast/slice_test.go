package ast

import "testing"

func TestBuiltinStageNamesIncludesLimitAndOffset(t *testing.T) {
	for _, name := range []string{"limit", "offset"} {
		if !BuiltinStageNames[name] {
			t.Errorf("BuiltinStageNames[%q] = false, want true", name)
		}
	}
}

func TestLimitOffsetShape(t *testing.T) {
	l := &Limit{N: 1000}
	if l.N != 1000 {
		t.Errorf("Limit.N = %d, want 1000", l.N)
	}
	o := &Offset{N: 50}
	if o.N != 50 {
		t.Errorf("Offset.N = %d, want 50", o.N)
	}
}
