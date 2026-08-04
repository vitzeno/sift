package checker

import (
	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/value"
)

// checkRouteTerminal validates a route {...} terminal (design-routing.md
// §6): every non-else branch's predicate must be bool over the terminal
// schema (type-checked exactly like a filter predicate, including
// rejecting an undischarged Optional bool — design/optional-fields.md
// §3's third discharge rule applies here too), else is mandatory (§2 —
// it's the only way arbitrary boolean predicates can be proven total),
// and every non-discard target must resolve to a declared sink.
//
// decision: "else is mandatory" is checked as "at least one branch has
// IsElse" rather than "the last branch is else". Wherever it appears,
// an else branch already guarantees every row reaching that point
// matches something, so totality holds regardless of position; branches
// after it would simply be unreachable, and design-routing.md §8
// explicitly declines to build reachability/overlap warnings for that.
// This keeps the check to the one property that actually matters.
//
// Returns the resolved branches (Target as a name, not a decl pointer —
// see RouteBranch's doc) plus every distinct target sink in
// first-occurrence order, for CheckedProgram.Sinks. Unlike
// checkNoDuplicateSink's broadcast rule, the same sink naming more than
// one branch is allowed (design-routing.md §6) — this dedups rather than
// rejecting a repeat.
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
