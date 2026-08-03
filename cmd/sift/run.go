package main

import (
	"os"
	"path/filepath"

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
// lets `go run ./cmd/sift run testdata/adults.sift` (CLAUDE.md's
// documented command) work unmodified from the repo root.
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
	sink := *cp.Sink
	sink.Path = resolvePath(base, sink.Path)

	top, runSink, err := runtime.Build(runtime.BuildInput{
		Source:       &source,
		SourceSchema: cp.SourceSchema,
		Sink:         &sink,
		SinkSchema:   cp.SinkSchema,
		Stages:       cp.Stages,
	})
	if err != nil {
		return err
	}

	return runtime.Run(top, runSink)
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
