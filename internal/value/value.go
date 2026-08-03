// Package value defines Sift's runtime row representation and its static
// type model, including the @pii tag that the checker enforces (design.md §3).
package value

import "strings"

// Kind is a scalar type. v0 only needs the four primitives design.md's
// expression grammar can produce (string/int/double literals, bool from
// comparisons and && / ||).
type Kind int

const (
	String Kind = iota
	Int
	Double
	Bool
)

func (k Kind) String() string {
	switch k {
	case String:
		return "string"
	case Int:
		return "int"
	case Double:
		return "double"
	case Bool:
		return "bool"
	default:
		return "unknown"
	}
}

// Type is a scalar kind plus the @pii tag. PII propagation (design.md §3)
// is a property of Type, not Kind: `mask(x)` returns the same Kind with
// PII cleared, everything else preserves both.
type Type struct {
	Kind Kind
	PII  bool
}

func (t Type) String() string {
	if t.PII {
		return t.Kind.String() + " @pii"
	}
	return t.Kind.String()
}

// Field is one named column of a Schema.
type Field struct {
	Name string
	Type Type
}

// Schema is the record type of a stream: an ordered list of fields.
// Order matters — it's what lets a sink write columns in a stable order
// even though Row.Fields is an unordered map (see jsonlSink).
type Schema struct {
	Fields []Field
}

// Lookup returns the field named name and whether it exists.
func (s Schema) Lookup(name string) (Field, bool) {
	for _, f := range s.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// FirstPII returns the first field still tagged @pii, in declared order,
// and whether one was found. The checker calls this once per sink to
// enforce design.md §3's rule 3 ("sinks reject unmasked PII") — checking
// in declared order keeps which field gets named in the error message
// deterministic.
func (s Schema) FirstPII() (Field, bool) {
	for _, f := range s.Fields {
		if f.Type.PII {
			return f, true
		}
	}
	return Field{}, false
}

// String renders the schema the way design.md §3 writes it:
// { name: string, age: int }. --emit-schema (module 8) will just call this.
func (s Schema) String() string {
	var b strings.Builder
	b.WriteString("{ ")
	for i, f := range s.Fields {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(f.Name)
		b.WriteString(": ")
		b.WriteString(f.Type.String())
	}
	b.WriteString(" }")
	return b.String()
}

// Provenance records where a row came from: the declared source name, its
// ordinal position in the stream (0-based), and a format-specific offset
// (for CSV, the 1-based line number of the record).
type Provenance struct {
	Source  string
	Ordinal int
	Offset  int
}

// Row is one record flowing through the pipeline: named field values plus
// provenance. Fields is a map (not an ordered struct) because a stage like
// map can add or drop columns freely — schema order lives on Schema, not Row.
type Row struct {
	Fields map[string]any
	Prov   Provenance
}
