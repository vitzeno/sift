package main

import (
	"os"
	"path/filepath"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/checker"
	"github.com/vitzeno/sift/internal/parser"
	"github.com/vitzeno/sift/internal/runtime"
)

// runFile lexes, parses, checks, builds, and executes the program at
// path — design.md §4's full compilation pipeline end to end.
//
// decision: a source or sink's path (e.g. csv("people.csv", ...))
// resolves relative to path's own directory, not the process's current
// working directory — so `sift run some/dir/x.sift` finds
// `some/dir/people.csv` regardless of where it's invoked from. An
// already-absolute path is left untouched. This matches how a shell
// script or Makefile resolves paths relative to itself, and is what
// lets `go run ./cmd/sift run examples/adults.sift` (CLAUDE.md's
// documented command) work unmodified from the repo root. The same
// resolution applies to an `on error |> <name>` route target's path.
func runFile(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	prog, err := parser.Parse(string(src))
	if err != nil {
		return err
	}

	cp, err := checker.Check(prog)
	if err != nil {
		return err
	}

	base := filepath.Dir(path)
	source := *cp.Source
	source.Path = resolvePath(base, source.Path)
	sinks := make([]*ast.SinkDecl, len(cp.Sinks))
	for i, s := range cp.Sinks {
		sink := *s
		sink.Path = resolvePath(base, sink.Path)
		sinks[i] = &sink
	}

	top, runSrc, runSinks, runRoute, err := runtime.Build(runtime.BuildInput{
		Source:       &source,
		SourceSchema: cp.SourceSchema,
		Sinks:        sinks,
		SinkSchema:   cp.SinkSchema,
		Stages:       cp.Stages,
		Route:        toRouteInputs(cp.Route),
	})
	if err != nil {
		return err
	}

	var errSink runtime.Sink
	if cp.ErrorPolicy == ast.ErrorRoute {
		errSinkDecl := *cp.ErrorSink
		errSinkDecl.Path = resolvePath(base, errSinkDecl.Path)
		errSink, err = runtime.NewErrorSink(&errSinkDecl)
		if err != nil {
			return err
		}
	}

	return runtime.Run(top, runSrc, runSinks, runRoute, toRuntimePolicy(cp.ErrorPolicy), errSink)
}

// toRouteInputs bridges checker.RouteBranch to runtime.RouteInput — the
// same field-for-field mirror BuildInput's other fields already draw
// between the two packages (build.go's decision comment), kept as one
// small conversion here since Route is nil for every non-routed program
// and this is the one place that needs to know both types.
func toRouteInputs(route []checker.RouteBranch) []runtime.RouteInput {
	if route == nil {
		return nil
	}
	in := make([]runtime.RouteInput, len(route))
	for i, b := range route {
		in[i] = runtime.RouteInput{Pred: b.Pred, IsElse: b.IsElse, Target: b.Target, Discard: b.Discard}
	}
	return in
}

// toRuntimePolicy translates the checker's frontend-facing
// ast.ErrorPolicyKind into runtime's own Policy — the same small
// same-shaped-enum bridge BuildInput already draws between checker and
// runtime (see build.go's decision comment): runtime doesn't depend on
// checker, and the checker has no reason to depend on runtime just to
// spell out three constants it doesn't otherwise need.
func toRuntimePolicy(k ast.ErrorPolicyKind) runtime.Policy {
	switch k {
	case ast.ErrorSkip:
		return runtime.PolicySkip
	case ast.ErrorRoute:
		return runtime.PolicyRoute
	default:
		return runtime.PolicyAbort
	}
}

// resolvePath joins path onto base unless path is already absolute, in
// which case it's returned unchanged — an absolute path always means
// exactly that, regardless of where the script lives.
func resolvePath(base, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
}
