// This file is package runtime_test, not runtime: internal/format
// imports internal/runtime to register with it, so a build_test.go
// inside package runtime importing internal/format for its init() side
// effect would be a real import cycle. An external test package is a
// separate compilation unit that can import both sides.
package runtime_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vitzeno/sift/internal/checker"
	"github.com/vitzeno/sift/internal/eval"
	_ "github.com/vitzeno/sift/internal/format" // registers "csv"/"jsonl" via init()
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

// TestBuildFullPipelineAdultsFilter is design.md §7 Case A, this time
// proven through the complete compiler pipeline (lex, parse, check,
// build, execute) rather than module 2's hand-wired Go chain or module
// 6's checker-only assertions. Every module from 1 through 7 takes part
// in producing this one output file.
//
// decision: the source path here is "../../testdata/people.csv",
// relative to this package's test working directory, not the bare
// "people.csv" design.md §7 writes (and that testdata/adults.sift uses).
// Resolving a source path relative to the .sift file's own directory is
// a real CLI concern for module 8, not yet built; this test only needs
// *a* path that resolves correctly from here.
func TestBuildFullPipelineAdultsFilter(t *testing.T) {
	src := `source in = csv("../../testdata/people.csv", schema: { name: string, age: int })
sink out = jsonl("` + filepath.ToSlash(t.TempDir()+"/adults.jsonl") + `")

pipeline main {
  in |> filter(.age >= 18) |> out
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

	got, err := os.ReadFile(cp.Sinks[0].Path)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	want := `{"name":"Ada","age":42}` + "\n"
	if string(got) != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestBuildFullPipelinePIIRejectedBeforeBuild confirms Case B's failure
// mode holds across the real parser+checker, not just hand-built ASTs:
// Build is never reached at all, since Check fails first.
func TestBuildFullPipelinePIIRejectedBeforeBuild(t *testing.T) {
	src := `source in = csv("../../testdata/people.csv", schema: { name: string, age: int, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> out
}`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if _, err := checker.Check(prog); err == nil {
		t.Fatal("Check succeeded, want it to reject the unmasked @pii field")
	}
}

// TestBuildFullPipelineRenamePreservesPII is S2's own demo
// (design-improvements.md §8): renaming a @pii column and writing it
// unmasked is still rejected, proof the tag survived the rename, through
// the real parser and checker rather than a hand-built AST.
func TestBuildFullPipelineRenamePreservesPII(t *testing.T) {
	src := `source in = csv("../../testdata/people.csv", schema: { name: string, age: int, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> rename(email: email_addr) |> out
}`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if _, err := checker.Check(prog); err == nil {
		t.Fatal("Check succeeded, want it to reject the renamed field, still @pii and unmasked")
	}
}

// TestBuildFullPipelineDeclassifyStage is S4's own demo
// (design-improvements.md §8): `|> hash(email) |> out` compiles and
// runs where the bare email column previously could not reach the sink
// at all, through the real compiler pipeline.
func TestBuildFullPipelineDeclassifyStage(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "people.csv")
	if err := os.WriteFile(inPath, []byte("name,email\nAda,ada@example.com\n"), 0o644); err != nil {
		t.Fatalf("writing fixture CSV: %v", err)
	}
	outPath := filepath.Join(dir, "out.jsonl")

	src := `source in = csv("` + filepath.ToSlash(inPath) + `", schema: { name: string, email: string @pii })
sink out = jsonl("` + filepath.ToSlash(outPath) + `")

pipeline main {
  in |> hash(email) |> out
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

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if strings.Contains(string(got), "ada@example.com") {
		t.Errorf("output = %q, want the raw email replaced by its hash", got)
	}
	want := `{"name":"Ada","email":"` + eval.Declassify("hash", "ada@example.com") + `"}` + "\n"
	if string(got) != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestBuildFullPipelineMapFilterCheck exercises all three v0 stages
// together through the full pipeline, including a named segment
// (design.md §2) inlined by the checker before Build ever sees it. It
// uses its own small CSV fixture (with an email column check/map need)
// rather than testdata/people.csv, which only has name/age.
func TestBuildFullPipelineMapFilterCheck(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "people.csv")
	if err := os.WriteFile(inPath, []byte("name,age,email\nAda,42,  ADA@EXAMPLE.COM  \nTom,15,tom@example.com\n"), 0o644); err != nil {
		t.Fatalf("writing fixture CSV: %v", err)
	}
	outPath := filepath.Join(dir, "out.jsonl")

	src := `pipeline clean =
     check(.email != "", "missing email")
  |> map({ ...row, email: lower(trim(.email)) })

source in = csv("` + filepath.ToSlash(inPath) + `", schema: { name: string, age: int, email: string })
sink out = jsonl("` + filepath.ToSlash(outPath) + `")

pipeline main {
  in |> clean |> filter(.age >= 18) |> out
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

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	want := `{"name":"Ada","age":42,"email":"ada@example.com"}` + "\n"
	if string(got) != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestBuildFullPipelineRouteResolvesSinkNamesToIndexes is
// design-routing.md end to end through the real compiler pipeline: Build
// must resolve each branch's target sink *name* into the right index of
// the sinks slice it constructs itself. That's the one piece of route
// wiring unique to this layer: checker only deals in names, and the
// driver only deals in indexes.
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

// TestBuildFullPipelineDropSatisfiesPIIRule is S1's own demo
// (design-improvements.md §8): dropping a @pii column reaches the sink
// legally with no mask/hash/redact at all, through the real compiler
// pipeline, not just the checker in isolation.
func TestBuildFullPipelineDropSatisfiesPIIRule(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "people.csv")
	if err := os.WriteFile(inPath, []byte("name,age,email\nAda,42,ada@example.com\n"), 0o644); err != nil {
		t.Fatalf("writing fixture CSV: %v", err)
	}
	outPath := filepath.Join(dir, "out.jsonl")

	src := `source in = csv("` + filepath.ToSlash(inPath) + `", schema: { name: string, age: int, email: string @pii })
sink out = jsonl("` + filepath.ToSlash(outPath) + `")

pipeline main {
  in |> drop(email) |> out
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

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	want := `{"name":"Ada","age":42}` + "\n"
	if string(got) != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}
