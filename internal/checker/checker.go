// Package checker validates a parsed *ast.Program and lowers it into a
// CheckedProgram module 7 (build + execute) can interpret directly: it
// resolves source/sink/pipeline names into one namespace, recomputes the
// schema at every stage (design.md §3 — "the checker's core job"),
// inlines named-segment references into a flat stage chain, and enforces
// that no @pii field reaches a sink unmasked (design.md §3, rule 3).
package checker

import (
	"fmt"
	"strings"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
	"github.com/vitzeno/sift/internal/value"
)

// CheckError is a checker diagnostic: a position plus a message, the same
// shape as parser.ParseError so the CLI (module 8) can format either one
// identically.
type CheckError struct {
	Pos lexer.Pos
	Msg string
}

func (e *CheckError) Error() string {
	return fmt.Sprintf("%s: %s", e.Pos, e.Msg)
}

func errorf(pos lexer.Pos, format string, args ...any) *CheckError {
	return &CheckError{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

// CheckedProgram is what module 7 builds and runs: the resolved source
// and sink(s) with their schemas, and the flat chain of stages between
// them. Stages contains only *ast.Filter/*ast.Map/*ast.Check — every
// *ast.NameRef to a named pipeline segment has already been inlined
// (stage.go's expandStages), and the source/sink boundary NameRefs are
// represented by the Source/Sinks fields instead of appearing in the
// list.
//
// Sinks holds one or more sinks in declared order (design-multisink.md's
// terminal broadcast: `|> out, out_2`). They all share the one
// SinkSchema below — broadcast performs no transform, so there's nothing
// to compute per sink.
//
// ErrorPolicy defaults to ast.ErrorAbort when the program declares none
// (design-errors.md §5). ErrorSink is non-nil only when ErrorPolicy is
// ast.ErrorRoute, and carries no schema of its own — it's always built
// against the fixed envelope (design-errors.md §4).
type CheckedProgram struct {
	Source       *ast.SourceDecl
	SourceSchema value.Schema
	Sinks        []*ast.SinkDecl
	SinkSchema   value.Schema
	Stages       []ast.Stage
	ErrorPolicy  ast.ErrorPolicyKind
	ErrorSink    *ast.SinkDecl
}

// declKind classifies a name in the program-wide namespace: a source,
// sink, and pipeline decl all share one namespace because a bare
// *ast.NameRef inside a pipeline body can resolve to any of the three
// (design.md §2's `in |> filter(...) |> out` and `in |> clean |> out`
// use identical syntax for "the source", "the sink", and "a named
// segment").
type declKind int

const (
	declSource declKind = iota
	declSink
	declPipeline
)

type checker struct {
	prog *ast.Program

	namespace       map[string]declKind
	sourcesByName   map[string]*ast.SourceDecl
	sinksByName     map[string]*ast.SinkDecl
	pipelinesByName map[string]*ast.PipelineDecl
	sourceSchemas   map[string]value.Schema
}

// Check validates prog and returns the program ready for module 7 to
// execute, or the first error found. Like the parser (module 5), the
// checker stops at the first error rather than collecting a batch: v0
// has no error-recovery/synchronization logic, and adding it wouldn't
// change whether any program passes or fails, only how much is reported
// per run.
//
// decision: unlike the parser, this package uses plain returned errors
// throughout rather than panic/recover. The parser's grammar functions
// call each other dozens of levels deep with no natural place to check
// an error in between; the checker's phases (namespace, schemas,
// stage expansion, PII check) are a short, flat sequence where
// `if err != nil { return nil, err }` costs nothing in readability.
func Check(prog *ast.Program) (*CheckedProgram, error) {
	c := &checker{prog: prog}

	if err := c.buildNamespace(); err != nil {
		return nil, err
	}
	if err := c.resolveSourceSchemas(); err != nil {
		return nil, err
	}
	errPolicy, errSink, err := c.resolveErrorPolicy()
	if err != nil {
		return nil, err
	}

	runnable, err := c.findRunnablePipeline()
	if err != nil {
		return nil, err
	}

	srcName := runnable.Body[0].(*ast.NameRef).Name
	sinkRefs := c.trailingSinkRefs(runnable)
	if err := c.checkNoDuplicateSink(sinkRefs); err != nil {
		return nil, err
	}

	middle := runnable.Body[1 : len(runnable.Body)-len(sinkRefs)]
	stages, schema, err := c.expandStages(middle, c.sourceSchemas[srcName], map[string]bool{}, runnable.Name)
	if err != nil {
		return nil, err
	}

	if f, ok := schema.FirstPII(); ok {
		return nil, errorf(runnable.Pos,
			"field %q is @pii and reaches sink %s unmasked; declassify with mask/hash/redact",
			f.Name, sinkNameList(sinkRefs))
	}

	sinks := make([]*ast.SinkDecl, len(sinkRefs))
	for i, ref := range sinkRefs {
		sinks[i] = c.sinksByName[ref.Name]
	}

	return &CheckedProgram{
		Source:       c.sourcesByName[srcName],
		SourceSchema: c.sourceSchemas[srcName],
		Sinks:        sinks,
		SinkSchema:   schema,
		Stages:       stages,
		ErrorPolicy:  errPolicy,
		ErrorSink:    errSink,
	}, nil
}

// trailingSinkRefs collects the terminal broadcast list
// (design-multisink.md §2) off the end of runnable's body: the run of
// consecutive *ast.NameRef elements, from the last backward, that each
// resolve to a declared sink. It never looks at index 0 (always the
// source ref) — findRunnablePipeline already guarantees the final
// element is a sink NameRef, so this always returns at least one.
func (c *checker) trailingSinkRefs(runnable *ast.PipelineDecl) []*ast.NameRef {
	body := runnable.Body
	sinkRefs := []*ast.NameRef{body[len(body)-1].(*ast.NameRef)}
	for i := len(body) - 2; i > 0; i-- {
		ref, ok := body[i].(*ast.NameRef)
		if !ok || c.namespace[ref.Name] != declSink {
			break
		}
		sinkRefs = append(sinkRefs, ref)
	}
	for l, r := 0, len(sinkRefs)-1; l < r; l, r = l+1, r-1 {
		sinkRefs[l], sinkRefs[r] = sinkRefs[r], sinkRefs[l]
	}
	return sinkRefs
}

// checkNoDuplicateSink rejects the same sink appearing twice in a
// broadcast list (design-multisink.md §5): every listed sink receives
// every row, so a repeat is a literal double-write, always a mistake.
// Contrast design-routing.md, where the same sink across branches is
// fine — that's per-row selection, not broadcast.
func (c *checker) checkNoDuplicateSink(sinkRefs []*ast.NameRef) error {
	seen := map[string]bool{}
	for _, ref := range sinkRefs {
		if seen[ref.Name] {
			return errorf(ref.Pos, "sink %q listed twice", ref.Name)
		}
		seen[ref.Name] = true
	}
	return nil
}

// sinkNameList formats a broadcast list for a diagnostic: a single sink
// unquoted-joined the way the pre-multisink message already did, or a
// comma-separated quoted list when there's more than one — the PII check
// runs once against the shared terminal schema (design-multisink.md §4),
// so one error must name every sink it covers.
func sinkNameList(sinkRefs []*ast.NameRef) string {
	if len(sinkRefs) == 1 {
		return fmt.Sprintf("%q", sinkRefs[0].Name)
	}
	names := make([]string, len(sinkRefs))
	for i, ref := range sinkRefs {
		names[i] = fmt.Sprintf("%q", ref.Name)
	}
	return strings.Join(names, ", ")
}

// buildNamespace resolves every source/sink/pipeline name into one map,
// rejecting a name declared more than once regardless of which of the
// three categories it's declared in.
func (c *checker) buildNamespace() error {
	c.namespace = map[string]declKind{}
	c.sourcesByName = map[string]*ast.SourceDecl{}
	c.sinksByName = map[string]*ast.SinkDecl{}
	c.pipelinesByName = map[string]*ast.PipelineDecl{}

	declare := func(name string, kind declKind, pos lexer.Pos) error {
		if _, exists := c.namespace[name]; exists {
			return errorf(pos, "%q is already declared", name)
		}
		c.namespace[name] = kind
		return nil
	}

	for _, s := range c.prog.Sources {
		if err := declare(s.Name, declSource, s.Pos); err != nil {
			return err
		}
		c.sourcesByName[s.Name] = s
	}
	for _, s := range c.prog.Sinks {
		if err := declare(s.Name, declSink, s.Pos); err != nil {
			return err
		}
		c.sinksByName[s.Name] = s
	}
	for _, p := range c.prog.Pipelines {
		// A named segment invoked as a bare NameRef (no parens) would be
		// ambiguous with a built-in stage of the same name written with
		// parens elsewhere in the same program (design-improvements.md
		// §7) — reject the shadow outright rather than leave a footgun.
		if ast.BuiltinStageNames[p.Name] {
			return errorf(p.Pos, "%q is a built-in stage name", p.Name)
		}
		if err := declare(p.Name, declPipeline, p.Pos); err != nil {
			return err
		}
		if err := c.checkParamList(p); err != nil {
			return err
		}
		c.pipelinesByName[p.Name] = p
	}
	return nil
}

// checkParamList validates a segment's parameter list once, at
// declaration time, regardless of whether any call site ever
// instantiates it -- the same "validate the shape once, upfront" spirit
// as resolveSourceSchemas checking every source's schema literal. Two
// things can't wait for a call site: a duplicate parameter name (always
// wrong, no substitution needed to see it), and a scalar parameter's
// type name not being one of the four v0 scalars.
func (c *checker) checkParamList(p *ast.PipelineDecl) error {
	seen := map[string]bool{}
	for _, param := range p.Params {
		if seen[param.Name] {
			return errorf(param.Pos, "duplicate parameter %q in segment %q", param.Name, p.Name)
		}
		seen[param.Name] = true
		if param.Kind == ast.ParamScalar {
			if _, ok := typeNames[param.TypeName]; !ok {
				return errorf(param.Pos, "unknown type %q for parameter %q (expected string, int, double, or bool)", param.TypeName, param.Name)
			}
		}
	}
	return nil
}

// findRunnablePipeline picks the one pipeline shaped source |> ... |>
// sink out of the program — CLAUDE.md's v0 scope is "a single linear
// pipeline", so exactly one such shape must exist. Every other
// PipelineDecl is a named segment, meant only to be referenced by
// NameRef from within this one.
func (c *checker) findRunnablePipeline() (*ast.PipelineDecl, error) {
	var found *ast.PipelineDecl
	for _, pd := range c.prog.Pipelines {
		if len(pd.Body) < 2 {
			continue
		}
		first, ok := pd.Body[0].(*ast.NameRef)
		if !ok || c.namespace[first.Name] != declSource {
			continue
		}
		last, ok := pd.Body[len(pd.Body)-1].(*ast.NameRef)
		if !ok || c.namespace[last.Name] != declSink {
			continue
		}
		if found != nil {
			return nil, errorf(pd.Pos,
				"found more than one runnable pipeline (%q and %q); v0 supports a single linear pipeline",
				found.Name, pd.Name)
		}
		found = pd
	}
	if found == nil {
		return nil, errorf(c.prog.Pos,
			"no runnable pipeline found (expected one pipeline shaped source |> ... |> sink)")
	}
	return found, nil
}
