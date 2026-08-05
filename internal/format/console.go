package format

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/vitzeno/sift/internal/runtime"
	"github.com/vitzeno/sift/internal/value"
)

func init() {
	runtime.RegisterSink("console", NewConsoleSink)
}

// consoleSink is --print's replacement for a program's declared sinks
// (cmd/sift): no file, no Path. It buffers every row (the same one-JSON-
// object-per-line shape jsonlSink writes) instead of writing them as
// they arrive, and prints the whole block, labeled by name, on Close.
// Buffering matters once a program has more than one sink (broadcast or
// route): printing per row as the driver visits each sink would
// interleave rows from different sinks into an unreadable stream.
type consoleSink struct {
	name   string
	schema value.Schema
	lines  [][]byte
}

func NewConsoleSink(opts runtime.SinkOptions) (runtime.Sink, error) {
	return &consoleSink{name: opts.Name, schema: opts.Schema}, nil
}

func (s *consoleSink) Write(row value.Row) error {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, field := range s.schema.Fields {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(field.Name)
		if err != nil {
			return err
		}
		buf.Write(key)
		buf.WriteByte(':')
		val, err := json.Marshal(row.Fields[field.Name])
		if err != nil {
			return fmt.Errorf("console sink: field %q: %w", field.Name, err)
		}
		buf.Write(val)
	}
	buf.WriteByte('}')
	s.lines = append(s.lines, buf.Bytes())
	return nil
}

func (s *consoleSink) Close() error {
	if _, err := fmt.Fprintf(os.Stdout, "=== %s ===\n", s.name); err != nil {
		return err
	}
	for _, line := range s.lines {
		if _, err := fmt.Fprintln(os.Stdout, string(line)); err != nil {
			return err
		}
	}
	return nil
}
