package format

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/vitzeno/sift/internal/runtime"
	"github.com/vitzeno/sift/internal/value"
)

func init() {
	runtime.RegisterSink("jsonl", NewJSONLSink)
}

// jsonlSink writes one JSON object per line. It needs the schema at
// construction purely for field order: Row.Fields is a map, and
// encoding/json would otherwise marshal map keys alphabetically
// ("age" before "name"), which doesn't match the source's declared
// column order. Writing field-by-field in schema order keeps output
// deterministic and matching the declared schema, byte for byte.
type jsonlSink struct {
	f      *os.File
	w      *bufio.Writer
	schema value.Schema
}

func NewJSONLSink(opts runtime.SinkOptions) (runtime.Sink, error) {
	f, err := os.Create(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("jsonl sink: %w", err)
	}
	return &jsonlSink{
		f:      f,
		w:      bufio.NewWriter(f),
		schema: opts.Schema,
	}, nil
}

func (s *jsonlSink) Write(row value.Row) error {
	s.w.WriteByte('{')
	for i, field := range s.schema.Fields {
		if i > 0 {
			s.w.WriteByte(',')
		}
		key, err := json.Marshal(field.Name)
		if err != nil {
			return err
		}
		s.w.Write(key)
		s.w.WriteByte(':')
		val, err := json.Marshal(row.Fields[field.Name])
		if err != nil {
			return fmt.Errorf("jsonl sink: field %q: %w", field.Name, err)
		}
		s.w.Write(val)
	}
	s.w.WriteByte('}')
	s.w.WriteByte('\n')
	return nil
}

func (s *jsonlSink) Close() error {
	if err := s.w.Flush(); err != nil {
		s.f.Close()
		return err
	}
	return s.f.Close()
}
