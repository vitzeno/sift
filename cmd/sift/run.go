package main

import (
	"os"

	"github.com/vitzeno/sift/internal/checker"
	"github.com/vitzeno/sift/internal/parser"
	"github.com/vitzeno/sift/internal/runtime"
)

// runFile lexes, parses, checks, builds, and executes the program at
// path — design.md §4's full compilation pipeline end to end.
//
// decision: a source's path (e.g. csv("people.csv", ...)) resolves
// relative to the process's current working directory, the same as any
// relative path passed to os.Open — not relative to path's own
// directory. Making a source path script-relative (so `sift run
// some/dir/x.sift` finds `some/dir/people.csv` regardless of the
// caller's cwd) is a real CLI convenience, but it's an added path-
// resolution feature, not something design.md or CLAUDE.md's v0 scope
// calls for. Kept as plain relative-to-cwd for v0; revisit if it's
// actually needed.
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

	top, sink, err := runtime.Build(runtime.BuildInput{
		Source:       cp.Source,
		SourceSchema: cp.SourceSchema,
		Sink:         cp.Sink,
		SinkSchema:   cp.SinkSchema,
		Stages:       cp.Stages,
	})
	if err != nil {
		return err
	}

	return runtime.Run(top, sink)
}
