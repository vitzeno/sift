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
//
// Sinks is a list, not a single decl — design-multisink.md's terminal
// broadcast: every sink receives the identical SinkSchema, since
// broadcast happens after the last stage and performs no transform. When
// Route is set (design-routing.md), Sinks instead holds every distinct
// sink a branch can target, and Route's entries name one of them by
// Target — Build resolves that name into a live index once it knows
// which position each named sink landed at in the sinks slice it builds.
type BuildInput struct {
	Source       *ast.SourceDecl
	SourceSchema value.Schema
	Sinks        []*ast.SinkDecl
	SinkSchema   value.Schema
	Stages       []ast.Stage
	Route        []RouteInput
}

// RouteInput is one route branch as BuildInput receives it — Build's own
// mirror of checker.RouteBranch, across the same package boundary every
// other BuildInput field already crosses. Target is a sink name, valid
// only until Build resolves it into a RouteBranch's SinkIndex.
type RouteInput struct {
	Pred    ast.Expr
	IsElse  bool
	Target  string
	Discard bool
}

// RouteBranch is one route branch after Build has resolved its target
// sink name into an index into the sinks slice Build also returns —
// design-routing.md §4's per-row branch walk reads this directly, no
// further name lookup needed at run time. SinkIndex is -1 when Discard.
type RouteBranch struct {
	Pred      ast.Expr
	IsElse    bool
	SinkIndex int
	Discard   bool
}

// Build turns a checked program into a runnable chain: a Source from the
// registry, wrapped by one runtime stage per checked ast.Stage, feeding
// every Sink also from the registry. This is design.md §4's "build"
// step — buildPipeline — the only place a *ast.Filter/*ast.Map/*ast.Check
// turns into the matching runtime.Filter/Map/Check.
//
// Build returns the original Source alongside the wrapped chain: Run
// needs both — top to pull rows, src to check Err() after top reports
// EOF (design-errors.md §2.4) — since a Filter/Map/Check wrapping src
// no longer looks like src to the type system. route is nil unless
// in.Route was set, resolved from in.Route's sink names into indexes
// into sinks — the one translation only Build can do, since it's the
// one place that knows which position each sink landed at.
func Build(in BuildInput) (top Stream, src Source, sinks []Sink, route []RouteBranch, err error) {
	src, err = NewSource(in.Source.Format, SourceOptions{
		Name:   in.Source.Name,
		Path:   in.Source.Path,
		Schema: in.SourceSchema,
		Opts:   sourceOptValues(in.Source.Opts),
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}

	sinks = make([]Sink, len(in.Sinks))
	sinkIndex := make(map[string]int, len(in.Sinks))
	for i, sinkDecl := range in.Sinks {
		sinks[i], err = NewSink(sinkDecl.Format, SinkOptions{
			Path:   sinkDecl.Path,
			Schema: in.SinkSchema,
		})
		if err != nil {
			return nil, nil, nil, nil, err
		}
		sinkIndex[sinkDecl.Name] = i
	}

	if in.Route != nil {
		route = make([]RouteBranch, len(in.Route))
		for i, b := range in.Route {
			idx := -1
			if !b.Discard {
				idx = sinkIndex[b.Target]
			}
			route[i] = RouteBranch{Pred: b.Pred, IsElse: b.IsElse, SinkIndex: idx, Discard: b.Discard}
		}
	}

	top = src
	for _, stage := range in.Stages {
		switch st := stage.(type) {
		case *ast.Filter:
			top = NewFilterExpr(top, st.Pred)
		case *ast.Map:
			top = NewMap(top, st.Record)
		case *ast.Check:
			top = NewCheck(top, st.Cond, st.Reason)
		case *ast.Select:
			top = NewSelect(top, columnNames(st.Columns))
		case *ast.Drop:
			top = NewDrop(top, columnNames(st.Columns))
		case *ast.Rename:
			top = NewRename(top, renamePairs(st.Pairs))
		case *ast.Limit:
			top = NewLimit(top, st.N)
		case *ast.Offset:
			top = NewOffset(top, st.N)
		case *ast.Declassify:
			top = NewDeclassify(top, st.Fn, columnNames(st.Columns))
		default:
			// The checker only ever emits ast.BuiltinStageNames kinds into
			// CheckedProgram.Stages (internal/checker/stage.go's
			// expandStages) — anything else means the checker and Build
			// have drifted out of sync with each other, an internal bug.
			panic(fmt.Sprintf("runtime: unhandled stage type %T", stage))
		}
	}

	return top, src, sinks, route, nil
}

// columnNames extracts the bare names from a column-ref list — Build
// only ever needs the names themselves; positions are a checker-time
// diagnostic concern the runtime layer has no use for.
func columnNames(cols []ast.ColumnRef) []string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return names
}

// sourceOptValues decodes a source declaration's extra keyword arguments
// (design/xlsx.md §1) into the plain-value map SourceOptions.Opts
// exposes to a format constructor. The parser guarantees every
// ast.SourceOpt.Value is one of the four scalar literal kinds
// (parseSourceOptValue), so the switch below is exhaustive by
// construction, not defensive coding against a case that can't occur.
func sourceOptValues(opts []ast.SourceOpt) map[string]any {
	if len(opts) == 0 {
		return nil
	}
	m := make(map[string]any, len(opts))
	for _, o := range opts {
		switch v := o.Value.(type) {
		case *ast.IntLit:
			m[o.Name] = v.Value
		case *ast.DoubleLit:
			m[o.Name] = v.Value
		case *ast.StringLit:
			m[o.Name] = v.Value
		case *ast.BoolLit:
			m[o.Name] = v.Value
		}
	}
	return m
}

// renamePairs flattens a rename stage's pair list into the old->new map
// NewRename needs — the checker has already ruled out any duplicate or
// colliding Old/New, so a plain map loses no information here.
func renamePairs(pairs []ast.RenamePair) map[string]string {
	m := make(map[string]string, len(pairs))
	for _, p := range pairs {
		m[p.Old] = p.New
	}
	return m
}
