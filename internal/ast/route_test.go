package ast

import "testing"

func TestBuiltinStageNamesIncludesRoute(t *testing.T) {
	if !BuiltinStageNames["route"] {
		t.Error(`BuiltinStageNames["route"] = false, want true`)
	}
}

func TestRouteTerminalShape(t *testing.T) {
	eu := &NameRef{Name: "eu_sink"}
	rt := &RouteTerminal{Branches: []RouteBranch{
		{Pred: &BoolLit{Value: true}, Target: eu},
		{IsElse: true, Discard: true},
	}}
	if len(rt.Branches) != 2 {
		t.Fatalf("Branches = %d, want 2", len(rt.Branches))
	}
	if rt.Branches[0].IsElse || rt.Branches[0].Discard || rt.Branches[0].Target != eu {
		t.Errorf("branch 0 = %+v, want a predicate branch targeting eu_sink", rt.Branches[0])
	}
	if !rt.Branches[1].IsElse || !rt.Branches[1].Discard || rt.Branches[1].Pred != nil {
		t.Errorf("branch 1 = %+v, want else => discard with a nil predicate", rt.Branches[1])
	}
}
