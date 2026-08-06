package jsonl_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vitzeno/sift/internal/format/csv"
	"github.com/vitzeno/sift/internal/format/jsonl"
	"github.com/vitzeno/sift/internal/runtime"
	"github.com/vitzeno/sift/internal/value"
)

// TestAdultsFilterEndToEnd is jsonlSink.Write/Close's only unit-level
// coverage -- jsonl has no other test file, so this drives it with real
// rows from a real csv.Source through the driver loop and Filter, rather
// than a synthetic Row built by hand (design.md §7 Case A, which this
// predates the parser for). cmd/sift's TestExampleFilter proves the same
// scenario through the real CLI; the two are different failure-isolation
// layers, not duplicates (CLAUDE.md's own unit-plus-CLI-acceptance
// convention). package jsonl_test, not jsonl, since nothing here needs
// jsonl's unexported internals -- only NewCSVSource and NewJSONLSink,
// both already exported.
func TestAdultsFilterEndToEnd(t *testing.T) {
	schema := value.Schema{Fields: []value.Field{
		{Name: "name", Type: value.Type{Kind: value.String}},
		{Name: "age", Type: value.Type{Kind: value.Int}},
	}}

	src, err := csv.NewCSVSource(runtime.SourceOptions{
		Name:   "in",
		Path:   "../../../testdata/people.csv",
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
