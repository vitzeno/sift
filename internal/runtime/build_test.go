// This file is package runtime_test, not runtime: internal/format's
// subpackages import internal/runtime to register with it, so a
// build_test.go inside package runtime importing one of them for its
// init() side effect would be a real import cycle. An external test
// package is a separate compilation unit that can import both sides.
package runtime_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vitzeno/sift/internal/checker"
	_ "github.com/vitzeno/sift/internal/format/csv"   // registers "csv" via init()
	_ "github.com/vitzeno/sift/internal/format/jsonl" // registers "jsonl" via init()
	"github.com/vitzeno/sift/internal/parser"
	"github.com/vitzeno/sift/internal/runtime"
)

// toBuildInput adapts a *checker.CheckedProgram to runtime.BuildInput,
// the one place in tests that bridges the two packages Build keeps
// decoupled at the type level (see build.go's decision comment). Module
// 8's CLI will do the same thing in production code.
func toBuildInput(cp *checker.CheckedProgram) runtime.BuildInput {
	return runtime.BuildInput{
		Source:       cp.Source,
		SourceSchema: cp.SourceSchema,
		Sinks:        cp.Sinks,
		SinkSchema:   cp.SinkSchema,
		Stages:       cp.Stages,
		Route:        toRouteInputs(cp.Route),
	}
}

// toRouteInputs mirrors cmd/sift/run.go's own bridge from
// checker.RouteBranch to runtime.RouteInput. nil for every non-routed
// program.
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

// TestBuildFullPipelineRouteResolvesSinkNamesToIndexes is
// design-routing.md end to end through the real compiler pipeline: Build
// must resolve each branch's target sink *name* into the right index of
// the sinks slice it constructs itself. That's the one piece of route
// wiring unique to this layer -- checker only deals in names, and the
// driver only deals in indexes -- and it's exercised here with sinks
// declared in the opposite order the route branches reference them,
// which no cmd/sift CLI-level route test happens to do; a name-to-index
// bug that only shows up under a reversed declaration order would slip
// past every other test in the suite.
func TestBuildFullPipelineRouteResolvesSinkNamesToIndexes(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "people.csv")
	if err := os.WriteFile(inPath, []byte("name,region\nAda,EU\nTom,US\nGrace,APAC\n"), 0o644); err != nil {
		t.Fatalf("writing fixture CSV: %v", err)
	}
	euPath := filepath.Join(dir, "eu.jsonl")
	restPath := filepath.Join(dir, "rest.jsonl")

	// Sinks declared in the opposite order routes reference them in,
	// proof this isn't secretly relying on declaration order lining up
	// with branch order.
	src := `source in = csv("` + filepath.ToSlash(inPath) + `", schema: { name: string, region: string })
sink rest_sink = jsonl("` + filepath.ToSlash(restPath) + `")
sink eu_sink = jsonl("` + filepath.ToSlash(euPath) + `")

pipeline main {
  in |> route {
    .region == "EU" => eu_sink,
    else            => rest_sink,
  }
}`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	cp, err := checker.Check(prog)
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	top, runSrc, sinks, route, err := runtime.Build(toBuildInput(cp))
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if err := runtime.Run(top, runSrc, sinks, route, runtime.PolicyAbort, nil); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	gotEU, err := os.ReadFile(euPath)
	if err != nil {
		t.Fatalf("reading eu output: %v", err)
	}
	if want := `{"name":"Ada","region":"EU"}` + "\n"; string(gotEU) != want {
		t.Errorf("eu output = %q, want %q", gotEU, want)
	}

	gotRest, err := os.ReadFile(restPath)
	if err != nil {
		t.Fatalf("reading rest output: %v", err)
	}
	want := `{"name":"Tom","region":"US"}` + "\n" + `{"name":"Grace","region":"APAC"}` + "\n"
	if string(gotRest) != want {
		t.Errorf("rest output = %q, want %q", gotRest, want)
	}
}
