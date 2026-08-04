package checker

import "github.com/vitzeno/sift/internal/ast"

// resolveErrorPolicy resolves the program's `on error` declaration
// (design-errors.md §5): absent means Abort (design.md §2's default;
// only "skip" is required to be explicit, not the default itself). A
// route target must resolve to an existing sink; naming a source,
// pipeline segment, or an undeclared name is a compile error.
//
// The error sink's inbound "schema" is the fixed envelope
// (design-errors.md §4), so unlike the main sink, its *ast.SinkDecl is
// carried forward as-is: no PII check, no schema recomputation. The
// checker never threads a pipeline schema into it.
func (c *checker) resolveErrorPolicy() (ast.ErrorPolicyKind, *ast.SinkDecl, error) {
	decl := c.prog.ErrorPolicy
	if decl == nil {
		return ast.ErrorAbort, nil, nil
	}
	if decl.Kind != ast.ErrorRoute {
		return decl.Kind, nil, nil
	}

	name := decl.Target.Name
	kind, exists := c.namespace[name]
	if !exists {
		return 0, nil, errorf(decl.Target.Pos, "undefined name %q", name)
	}
	if kind != declSink {
		return 0, nil, errorf(decl.Target.Pos, "%q is not a sink; the error route target must be a sink", name)
	}
	return ast.ErrorRoute, c.sinksByName[name], nil
}
