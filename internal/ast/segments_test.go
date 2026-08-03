package ast

import "testing"

func TestParamShape(t *testing.T) {
	col := Param{Name: "col", Kind: ParamColumn}
	scalar := Param{Name: "min", Kind: ParamScalar, TypeName: "int"}
	if col.Kind != ParamColumn || col.TypeName != "" {
		t.Errorf("col = %+v, want a bare ParamColumn with no TypeName", col)
	}
	if scalar.Kind != ParamScalar || scalar.TypeName != "int" {
		t.Errorf("scalar = %+v, want ParamScalar with TypeName int", scalar)
	}
}

func TestSegmentCallShape(t *testing.T) {
	call := &SegmentCall{
		Name: "scrub",
		Args: []CallArg{
			{Kind: ArgColumn, Column: "email"},
			{Kind: ArgScalar, Literal: &IntLit{Value: 18}},
		},
	}
	if len(call.Args) != 2 {
		t.Fatalf("Args = %+v, want 2 entries", call.Args)
	}
	if call.Args[0].Kind != ArgColumn || call.Args[0].Column != "email" {
		t.Errorf("Args[0] = %+v, want {ArgColumn, email}", call.Args[0])
	}
	if call.Args[1].Kind != ArgScalar {
		t.Errorf("Args[1] = %+v, want ArgScalar", call.Args[1])
	}
	lit, ok := call.Args[1].Literal.(*IntLit)
	if !ok || lit.Value != 18 {
		t.Errorf("Args[1].Literal = %#v, want *IntLit{Value: 18}", call.Args[1].Literal)
	}
}
