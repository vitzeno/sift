// Package csv registers a Source that reads CSV files against a declared
// schema. This is the only place that knows the string "csv"; the
// registry itself, and everything above it, is format-agnostic.
package csv

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/vitzeno/sift/internal/format"
	"github.com/vitzeno/sift/internal/runtime"
	"github.com/vitzeno/sift/internal/value"
)

func init() {
	runtime.RegisterSource("csv", NewCSVSource)
}

// csvSource reads rows against a declared schema: CSV has no inherent
// types, so the schema must be given up front. Column order in
// the file need not match the schema's declared order; csvSource matches
// columns by header name.
type csvSource struct {
	f    *os.File
	r    *csv.Reader
	name string
	// col maps a schema field name to its column index in the CSV file.
	col map[string]int
	// dateFormats holds the source's formats kwarg: a date-typed field
	// name to the layout string to parse it against.
	dateFormats map[string]string
	// deidentifyKey is the 32-byte AES-256 key resolved from the source's
	// key kwarg, nil for a schema with no @deidentify field.
	deidentifyKey []byte
	schema        value.Schema
	ordinal       int
	// err holds an infra-fatal error from the underlying reader (a
	// malformed record it couldn't tokenize at all, an I/O error mid-
	// read). It's infra-fatal, not a per-row Failure, since there's no
	// well-formed row to attach one to.
	err error
}

// NewCSVSource is the SourceCtor registered under "csv". It opens the file,
// reads the header row, and checks every required schema field has a
// matching column, by name or by the columns kwarg's alias. A missing
// required column is a construction-time error, not a panic, since it's
// a real misconfiguration a caller can hit legitimately (a typo'd
// schema, a header-less export, etc). An Optional field's column may be
// absent from the header entirely: every row simply reads it as Absent;
// only a required field's absence is structural.
func NewCSVSource(opts runtime.SourceOptions) (runtime.Source, error) {
	f, err := os.Open(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("csv source %q: %w", opts.Name, err)
	}

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("csv source %q: reading header: %w", opts.Name, err)
	}

	col, missing, ok := format.ResolveColumns(opts.Schema, header, opts.Columns)
	if !ok {
		f.Close()
		return nil, fmt.Errorf("csv source %q: required column %q not found in header %s", opts.Name, missing, strings.Join(header, ", "))
	}

	key, err := format.RequireDeidentifyKey(opts.Schema, opts.DeidentifyKeyEnvVar)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("csv source %q: %w", opts.Name, err)
	}

	return &csvSource{
		f:             f,
		r:             r,
		name:          opts.Name,
		col:           col,
		dateFormats:   opts.DateFormats,
		deidentifyKey: key,
		schema:        opts.Schema,
	}, nil
}

func (s *csvSource) Schema() value.Schema {
	return s.schema
}

func (s *csvSource) Err() error {
	return s.err
}

func (s *csvSource) Next() (value.Row, bool) {
	record, err := s.r.Read()
	if err == io.EOF {
		return value.Row{}, false
	}
	if err != nil {
		// The reader couldn't tokenize this record at all (a bad quote,
		// a field-count mismatch). There's no well-formed row to carry
		// a per-row Failure, so this is infra-fatal: stop the stream and
		// let the driver read it back via Err() after Next returns
		// ok == false.
		s.err = fmt.Errorf("csv source %q: %w", s.name, err)
		return value.Row{}, false
	}

	line, _ := s.r.FieldPos(0)
	prov := value.Provenance{
		Source:  s.name,
		Ordinal: s.ordinal,
		Offset:  line,
	}
	s.ordinal++

	fields := make(map[string]any, len(s.schema.Fields))
	for _, field := range s.schema.Fields {
		// idx is absent only for an Optional field whose column isn't in
		// the header at all (the header check above already rejected
		// that for a required field), in which case raw stays "" and
		// Coerce reads it as absent.
		var raw string
		if idx, ok := s.col[field.Name]; ok {
			raw = record[idx]
		}
		v, fail := value.Coerce(field.Type, raw, format.ResolveDateFormat(s.dateFormats, field.Name, format.DefaultFormatForKind(field.Type.Kind)), s.deidentifyKey)
		if fail != nil {
			// One failure per row: the first bad cell marks the row and
			// short-circuits. The rest of the record is never coerced.
			fail.Stage = fmt.Sprintf("csv:%s", field.Name)
			return value.Row{Fail: fail, Prov: prov}, true
		}
		fields[field.Name] = v
	}

	return value.Row{Fields: fields, Prov: prov}, true
}
