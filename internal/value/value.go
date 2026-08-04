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

// Type is a scalar kind plus its two orthogonal tags: @pii and optionality
// (design/optional-fields.md §4 — both propagate, both must be discharged
// independently before a sink). `mask(x)` returns the same Kind with PII
// cleared; `??` returns the same Kind with Optional cleared; everything
// else preserves both.
type Type struct {
	Kind     Kind
	Optional bool
	PII      bool
}

func (t Type) String() string {
	s := t.Kind.String()
	if t.Optional {
		s += "?"
	}
	if t.PII {
		s += " @pii"
	}
	return s
}

// Absent is the runtime value of a declared-optional field that had no
// value (design/optional-fields.md §1: absence is a well-typed value, not
// a failure and not a null). Coerce returns it for a missing column or an
// empty cell against an Optional Type; it is never confused with Go's nil.
type Absent struct{}

// MarshalJSON renders Absent as JSON null — a sink-format detail, not a
// language-level null (design/optional-fields.md §6 forbids null only at
// the language level).
func (Absent) MarshalJSON() ([]byte, error) {
	return []byte("null"), nil
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

// FirstOptional returns the first field still Optional, in declared
// order, and whether one was found — the discharge-rule mirror of
// FirstPII (design/optional-fields.md §3: "reaching a sink... mirrors the
// PII sink rule"). The checker calls this once per sink to reject a
// still-optional field the same way it rejects unmasked PII.
func (s Schema) FirstOptional() (Field, bool) {
	for _, f := range s.Fields {
		if f.Type.Optional {
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

// Failure marks a Row that failed somewhere in the pipeline — a check
// condition that was false, or (from design-errors.md's phase E3) a
// source cell that wouldn't coerce to its declared type. It never
// duplicates Provenance: the Row it rides on already carries source,
// ordinal, and offset (design-errors.md §2.1).
type Failure struct {
	Reason string // human-facing, e.g. "missing email"
	Stage  string // originating stage, e.g. "check", "csv:age"
}

// Row is one record flowing through the pipeline: named field values plus
// provenance. Fields is a map (not an ordered struct) because a stage like
// map can add or drop columns freely — schema order lives on Schema, not Row.
//
// Fail is nil for a healthy row. Once set, every stage downstream must
// treat the row as opaque and pass it through untouched — Fields may be
// incomplete or suspect, so no stage may evaluate an expression against
// it (design-errors.md §2.2). Only the driver, at the end of the chain,
// disposes of a failed row per the program's error policy.
type Row struct {
	Fields map[string]any
	Prov   Provenance
	Fail   *Failure
}
