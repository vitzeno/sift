package format

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vitzeno/sift/internal/runtime"
	"github.com/vitzeno/sift/internal/value"
)

// TestAdultsFilterEndToEnd is design.md §7 Case A, wired by hand (no
// lexer/parser yet): csvSource |> Filter |> jsonlSink, driven by the one
// driver loop. It asserts on the exact output bytes, matching
// CLAUDE.md's "Definition of done" for v0.
func TestAdultsFilterEndToEnd(t *testing.T) {
	schema := peopleSchema()

	src, err := NewCSVSource(runtime.SourceOptions{
		Name:   "in",
		Path:   "../../testdata/people.csv",
		Schema: schema,
	})
	if err != nil {
		t.Fatalf("NewCSVSource: %v", err)
	}

	outPath := filepath.Join(t.TempDir(), "adults.jsonl")
	sink, err := NewJSONLSink(runtime.SinkOptions{
		Path:   outPath,
		Schema: schema,
	})
	if err != nil {
		t.Fatalf("NewJSONLSink: %v", err)
	}

	adults := runtime.NewFilter(src, func(r value.Row) bool {
		return r.Fields["age"].(int) >= 18
	})

	if err := runtime.Run(adults, sink); err != nil {
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
