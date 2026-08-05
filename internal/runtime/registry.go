package runtime

import (
	"fmt"

	"github.com/vitzeno/sift/internal/value"
)

// SourceOptions and SinkOptions are the construction-time parameters a
// format needs. This is a plain struct rather than map[string]any: v0
// has exactly two formats and both need a path plus a declared schema,
// so a typed struct is simpler and catches typos at compile time.
//
// decision: once the parser/checker exist (modules 5-6), a source/sink
// declaration's keyword args (`csv("people.csv", schema: {...})`) will
// translate into one of these structs. The struct may grow fields for
// format-specific options (e.g. a CSV delimiter) but won't need a
// different shape, since the registry never special-cases a format.
type SourceOptions struct {
	// Name is the source's declared name (e.g. "in"), recorded in each
	// row's Provenance.Source.
	Name   string
	Path   string
	Schema value.Schema
	// Opts holds every source keyword argument beyond schema (e.g.
	// xlsx's "sheet"/"header_row", design/xlsx.md §1), decoded from
	// their ast.SourceOpt literals into plain Go values: string, int64,
	// float64, or bool. A format that needs no extra options (csv,
	// jsonl) never looks here. Only the registered constructor for a
	// given format interprets a key's meaning or validates its type;
	// the registry itself stays format-agnostic.
	Opts map[string]any
	// Columns holds the optional columns kwarg (design/column-aliases.md
	// §3): schema field name to the raw header string to match instead
	// of the field's own identifier. Nil for a source with no columns
	// kwarg. Only csv and xlsx look here.
	Columns map[string]string
	// DateFormats holds the optional formats kwarg (design/date.md §3):
	// a date-typed schema field name to the Go reference-layout string to
	// parse its cells against. A date field absent from this map (or a
	// nil map, for a source with no formats kwarg at all) uses
	// value.DefaultDateFormat. Only csv and xlsx look here.
	DateFormats map[string]string
}

type SinkOptions struct {
	// Name is the sink's declared name (e.g. "out"), unused by every
	// format that only needs Path and Schema. The console format (--print,
	// cmd/sift) is the one exception: it has no file to write, so Name is
	// what labels its printed block once a program has more than one sink.
	Name   string
	Path   string
	Schema value.Schema
}

// SourceCtor and SinkCtor are what a format registers under its name
// (design.md §4). Adding a format never touches the lexer, parser,
// checker, or executor, only a registry entry.
type SourceCtor func(SourceOptions) (Source, error)
type SinkCtor func(SinkOptions) (Sink, error)

var (
	sourceRegistry = map[string]SourceCtor{}
	sinkRegistry   = map[string]SinkCtor{}
)

// RegisterSource makes a format available under name. Formats call this
// from an init() in internal/format; the registry itself has no built-in
// knowledge of "csv" or "jsonl".
func RegisterSource(name string, ctor SourceCtor) {
	sourceRegistry[name] = ctor
}

func RegisterSink(name string, ctor SinkCtor) {
	sinkRegistry[name] = ctor
}

// NewSource looks up name and constructs a Source from opts.
func NewSource(name string, opts SourceOptions) (Source, error) {
	ctor, ok := sourceRegistry[name]
	if !ok {
		return nil, fmt.Errorf("no source format registered under %q", name)
	}
	return ctor(opts)
}

func NewSink(name string, opts SinkOptions) (Sink, error) {
	ctor, ok := sinkRegistry[name]
	if !ok {
		return nil, fmt.Errorf("no sink format registered under %q", name)
	}
	return ctor(opts)
}
