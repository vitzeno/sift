package console

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/vitzeno/sift/internal/runtime"
	"github.com/vitzeno/sift/internal/value"
)

// captureStdout redirects os.Stdout to a pipe for fn's duration,
// returning everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	return buf.String()
}

// TestConsoleSinkBuffersAndPrintsOnClose confirms the printed block is
// labeled by the sink's own name and renders rows in schema field order,
// the same shape jsonlSink writes to a file.
func TestConsoleSinkBuffersAndPrintsOnClose(t *testing.T) {
	schema := value.Schema{Fields: []value.Field{
		{Name: "name", Type: value.Type{Kind: value.String}},
		{Name: "age", Type: value.Type{Kind: value.Int}},
	}}
	sink, err := NewConsoleSink(runtime.SinkOptions{Name: "out", Schema: schema})
	if err != nil {
		t.Fatalf("NewConsoleSink: %v", err)
	}
	if err := sink.Write(value.Row{Fields: map[string]any{"name": "Ada", "age": 42}}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := sink.Write(value.Row{Fields: map[string]any{"name": "Liam", "age": 25}}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := captureStdout(t, func() {
		if err := sink.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	want := "=== out ===\n{\"name\":\"Ada\",\"age\":42}\n{\"name\":\"Liam\",\"age\":25}\n"
	if got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

// TestConsoleSinkNothingPrintedBeforeClose confirms rows are buffered,
// not streamed -- the reason this sink exists once a program has more
// than one sink, so their rows never interleave on the terminal.
func TestConsoleSinkNothingPrintedBeforeClose(t *testing.T) {
	schema := value.Schema{Fields: []value.Field{{Name: "name", Type: value.Type{Kind: value.String}}}}
	sink, err := NewConsoleSink(runtime.SinkOptions{Name: "out", Schema: schema})
	if err != nil {
		t.Fatalf("NewConsoleSink: %v", err)
	}

	got := captureStdout(t, func() {
		if err := sink.Write(value.Row{Fields: map[string]any{"name": "Ada"}}); err != nil {
			t.Fatalf("Write: %v", err)
		}
	})
	if got != "" {
		t.Errorf("stdout after Write (before Close) = %q, want empty", got)
	}
}
