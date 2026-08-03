package checker

import (
	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/value"
)

// expandStages walks a slice of stages -- either the runnable pipeline's
// middle section (between its leading source NameRef and trailing sink
// NameRef) or a named segment's entire body -- threading schema through
// each one and inlining any *ast.NameRef to another named segment.
//
// The return slice holds only built-in stage nodes (ast.BuiltinStageNames):
// by the time a NameRef is resolved here, it either expands into more of
// those (recursively) or fails, so nothing else can reach the output.
// This is exactly the flat chain module 7 needs to build Stream objects
// from.
//
// visiting is the in-progress call stack of segment names, used to
// reject a segment defined in terms of itself (directly or through a
// cycle of segments) with a clear error instead of a stack overflow --
// CLAUDE.md: "no bare panic on user-facing error paths", and an infinite
// recursion is exactly that if a malformed program can trigger it.
func (c *checker) expandStages(stages []ast.Stage, schema value.Schema, visiting map[string]bool) ([]ast.Stage, value.Schema, error) {
	var out []ast.Stage

	for _, s := range stages {
		switch st := s.(type) {
		case *ast.Filter:
			predType, err := c.checkExpr(st.Pred, schema)
			if err != nil {
				return nil, value.Schema{}, err
			}
			if predType.Kind != value.Bool {
				return nil, value.Schema{}, errorf(st.Pos, "filter predicate must be bool, got %s", predType)
			}
			out = append(out, st)

		case *ast.Check:
			condType, err := c.checkExpr(st.Cond, schema)
			if err != nil {
				return nil, value.Schema{}, err
			}
			if condType.Kind != value.Bool {
				return nil, value.Schema{}, errorf(st.Pos, "check condition must be bool, got %s", condType)
			}
			out = append(out, st)

		case *ast.Map:
			newSchema, err := c.checkMapRecord(st.Record, schema)
			if err != nil {
				return nil, value.Schema{}, err
			}
			out = append(out, st)
			schema = newSchema

		case *ast.Select:
			newSchema, err := c.checkSelect(st, schema)
			if err != nil {
				return nil, value.Schema{}, err
			}
			out = append(out, st)
			schema = newSchema

		case *ast.Drop:
			newSchema, err := c.checkDrop(st, schema)
			if err != nil {
				return nil, value.Schema{}, err
			}
			out = append(out, st)
			schema = newSchema

		case *ast.Rename:
			newSchema, err := c.checkRename(st, schema)
			if err != nil {
				return nil, value.Schema{}, err
			}
			out = append(out, st)
			schema = newSchema

		case *ast.NameRef:
			expanded, newSchema, err := c.expandNameRef(st, schema, visiting)
			if err != nil {
				return nil, value.Schema{}, err
			}
			out = append(out, expanded...)
			schema = newSchema

		default:
			panic("checker: unhandled stage type")
		}
	}
	return out, schema, nil
}

// expandNameRef resolves a NameRef found in the middle of a chain. Only
// a named pipeline segment is valid here: a source or sink name may only
// appear at the very ends of the one runnable pipeline (design.md's
// characterization of a source/sink-less pipeline as a pure
// `stream<T> -> stream<U>` value means a named segment's body can never
// itself touch a source or sink).
func (c *checker) expandNameRef(ref *ast.NameRef, schema value.Schema, visiting map[string]bool) ([]ast.Stage, value.Schema, error) {
	switch c.namespace[ref.Name] {
	case declSource:
		return nil, value.Schema{}, errorf(ref.Pos,
			"source %q cannot be referenced here; a source may only start the runnable pipeline", ref.Name)
	case declSink:
		return nil, value.Schema{}, errorf(ref.Pos,
			"sink %q cannot be referenced here; a sink may only end the runnable pipeline", ref.Name)
	case declPipeline:
		if visiting[ref.Name] {
			return nil, value.Schema{}, errorf(ref.Pos, "pipeline %q is defined in terms of itself", ref.Name)
		}
		visiting[ref.Name] = true
		defer delete(visiting, ref.Name)
		seg := c.pipelinesByName[ref.Name]
		return c.expandStages(seg.Body, schema, visiting)
	default:
		return nil, value.Schema{}, errorf(ref.Pos, "undefined name %q", ref.Name)
	}
}
