package format_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vitzeno/sift/internal/format/csv"
	"github.com/vitzeno/sift/internal/format/jsonl"
	"github.com/vitzeno/sift/internal/runtime"
	"github.com/vitzeno/sift/internal/value"
)

// TestAdultsFilterEndToEnd predates the parser (it wires
// csv.Source |> Filter |> jsonl.Sink by hand, design.md §7 Case A), but
// stays load-bearing rather than becoming a stale v0 artifact: it's the
// only non-CLI test that drives the real csv and jsonl formats together
// through the driver loop, and jsonl has no dedicated test file of its
// own -- this is jsonlSink.Write/Close's only unit-level coverage.
// cmd/sift's TestExampleFilter proves the same scenario through the real
// CLI; the two are different failure-isolation layers, not duplicates
// (CLAUDE.md's own unit-plus-CLI-acceptance convention).
func TestAdultsFilterEndToEnd(t *testing.T) {
	schema := value.Schema{Fields: []value.Field{
		{Name: "name", Type: value.Type{Kind: value.String}},
		{Name: "age", Type: value.Type{Kind: value.Int}},
	}}

	src, err := csv.NewCSVSource(runtime.SourceOptions{
		Name:   "in",
		Path:   "../../testdata/people.csv",
		Schema: schema,
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	outPath := filepath.Join(t.TempDir(), "adults.jsonl")
	sink, err := jsonl.NewJSONLSink(runtime.SinkOptions{
		Path:   outPath,
		Schema: schema,
	})
	if err != nil {
		t.Fatalf("NewJSONLSink: %v", err)
	}

	adults := runtime.NewFilter(src, func(r value.Row) bool {
		return r.Fields["age"].(int) >= 18
	})

	if err := runtime.Run(adults, src, []runtime.Sink{sink}, nil, runtime.PolicyAbort, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	const want = `{"name":"Ada","age":42}` + "\n"
	if string(got) != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}
