package runtime

import (
	"fmt"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/value"
)

// BuildInput is the runtime's own view of a checked program: just the
// pieces Build needs, expressed in terms this package already depends
// on (ast, value). It deliberately mirrors internal/checker.
// CheckedProgram field-for-field rather than importing that type
// directly — checker is a compiler-phase package built on top of ast and
// value, and runtime (the executor underneath everything) has no
// business depending on it. The five-field duplication is the price of
// keeping that dependency arrow pointing one way; the caller (module 8's
// CLI) is what bridges the two.
type BuildInput struct {
	Source       *ast.SourceDecl
	SourceSchema value.Schema
	Sink         *ast.SinkDecl
	SinkSchema   value.Schema
	Stages       []ast.Stage
}

// Build turns a checked program into a runnable chain: a Source from the
// registry, wrapped by one runtime stage per checked ast.Stage, feeding
// a Sink also from the registry. This is design.md §4's "build" step —
// buildPipeline — the only place a *ast.Filter/*ast.Map/*ast.Check turns
// into the matching runtime.Filter/Map/Check.
func Build(in BuildInput) (Stream, Sink, error) {
	src, err := NewSource(in.Source.Format, SourceOptions{
		Name:   in.Source.Name,
		Path:   in.Source.Path,
		Schema: in.SourceSchema,
	})
	if err != nil {
		return nil, nil, err
	}

	sink, err := NewSink(in.Sink.Format, SinkOptions{
		Path:   in.Sink.Path,
		Schema: in.SinkSchema,
	})
	if err != nil {
		return nil, nil, err
	}

	var stream Stream = src
	for _, stage := range in.Stages {
		switch st := stage.(type) {
		case *ast.Filter:
			stream = NewFilterExpr(stream, st.Pred)
		case *ast.Map:
			stream = NewMap(stream, st.Record)
		case *ast.Check:
			stream = NewCheck(stream, st.Cond, st.Reason)
		default:
			// The checker only ever emits these three stage kinds into
			// CheckedProgram.Stages (internal/checker/stage.go's
			// expandStages) — anything else means the checker and Build
			// have drifted out of sync with each other, an internal bug.
			panic(fmt.Sprintf("runtime: unhandled stage type %T", stage))
		}
	}

	return stream, sink, nil
}
