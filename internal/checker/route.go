package checker

import (
	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/value"
)

// checkRouteTerminal validates a route {...} terminal (design-routing.md
// §6): each branch's predicate must be bool (like a filter predicate,
// including the Optional-bool rule from design/optional-fields.md §3),
// else is required (§2), and each target must be a declared sink.
//
// decision: "else required" means "at least one branch has IsElse", not
// "the last branch is else". An else branch anywhere already makes every
// row match something; branches after it are just unreachable, which
// design-routing.md §8 says not to warn about.
//
// Returns the branches (Target is a name, see RouteBranch) plus every
// distinct sink they target, in first-seen order, for
// CheckedProgram.Sinks. Unlike broadcast, the same sink can appear in
// more than one branch (§6), so this dedups instead of rejecting repeats.
func (c *checker) checkRouteTerminal(rt *ast.RouteTerminal, schema value.Schema) ([]RouteBranch, []*ast.SinkDecl, error) {
	branches := make([]RouteBranch, len(rt.Branches))
	var sinks []*ast.SinkDecl
	seen := map[string]bool{}
	hasElse := false

	for i, b := range rt.Branches {
		rb := RouteBranch{IsElse: b.IsElse, Discard: b.Discard}
		if b.IsElse {
			hasElse = true
		} else {
			predType, err := c.checkExpr(b.Pred, schema)
			if err != nil {
				return nil, nil, err
			}
			if predType.Kind != value.Bool || predType.Optional {
				return nil, nil, errorf(b.Pos, "route branch predicate must be bool, got %s", predType)
			}
			rb.Pred = b.Pred
		}
		if !b.Discard {
			if c.namespace[b.Target.Name] != declSink {
				return nil, nil, errorf(b.Target.Pos, "route target %q is not a declared sink", b.Target.Name)
			}
			rb.Target = b.Target.Name
			if !seen[b.Target.Name] {
				seen[b.Target.Name] = true
				sinks = append(sinks, c.sinksByName[b.Target.Name])
			}
		}
		branches[i] = rb
	}

	if !hasElse {
		return nil, nil, errorf(rt.Pos,
			"route is not total; add an 'else' branch (use 'else => discard' to drop unmatched rows explicitly)")
	}
	return branches, sinks, nil
}
