// Package value defines Sift's runtime row representation and its static
// type model, including the @pii tag that the checker enforces (design.md §3).
package value

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Kind is a scalar type. v0 only needs the four primitives design.md's
// expression grammar produces: string/int/double literals, and bool from
// comparisons and && / ||. Date was added by design/date.md; Decimal by
// design/decimal.md.
type Kind int

const (
	String Kind = iota
	Int
	Double
	Bool
	Date
	Decimal
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
	case Date:
		return "date"
	case Decimal:
		return "decimal"
	default:
		return "unknown"
	}
}

// Type is a scalar kind plus two independent tags, @pii and optionality
// (design/optional-fields.md §4): both propagate and both must be cleared
// separately before a sink. `mask(x)` clears PII; `??` clears Optional;
// everything else preserves both.
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

// Absent is the runtime value of a declared-optional field with no value
// (design/optional-fields.md §1: absence is a well-typed value, not a
// failure and not a null). Coerce returns it for a missing column or an
// empty cell against an Optional Type. It is never Go's nil.
type Absent struct{}

// MarshalJSON renders Absent as JSON null. That's a sink-format detail,
// not a language-level null (design/optional-fields.md §6 forbids null
// only at the language level).
func (Absent) MarshalJSON() ([]byte, error) {
	return []byte("null"), nil
}

// DateValue is a calendar date: year, month, day, with no time-of-day or
// timezone (design/date.md §2). It wraps time.Time rather than aliasing
// it (`type DateValue time.Time`, not `= time.Time`) so a DateValue never
// silently inherits every time.Time method — most of them (timezone
// conversion, Now()-adjacent constructors) don't make sense for a
// date-only value. Converting back for comparison is an explicit
// time.Time(d) at the call site (design/date.md §3).
type DateValue time.Time

// String renders a DateValue the same way it's parsed and marshaled:
// ISO-8601 (design/date.md §3 locks this as the canonical rendering,
// independent of whatever format the source cell used).
func (d DateValue) String() string {
	return time.Time(d).Format("2006-01-02")
}

// MarshalJSON renders a DateValue as a plain ISO-8601 string, not
// time.Time's own RFC 3339 (which would leak a time-of-day component a
// date-only value never had) (design/date.md §3).
func (d DateValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// DecimalValue is an exact-arithmetic scalar (design/decimal.md §2). It
// wraps decimal.Decimal rather than aliasing it, mirroring DateValue's
// own reasoning exactly: decimal.Decimal's own String()/MarshalJSON
// silently strip trailing zeros ("5.00" renders as "5") even though the
// value's internal exponent still remembers the original scale
// (confirmed empirically, not assumed -- NewFromString("5.00").Exponent()
// is -2, but .String() drops it anyway). That contradicts §1's "what you
// parse is what comes back out." DecimalValue's own String()/MarshalJSON
// render at the value's own remembered scale instead, via StringFixed.
type DecimalValue decimal.Decimal

// String renders a DecimalValue at its own remembered scale (its
// exponent, from parsing or from arithmetic that changed it), not
// decimal.Decimal's own trailing-zero-stripping default.
func (d DecimalValue) String() string {
	dec := decimal.Decimal(d)
	places := -dec.Exponent()
	if places < 0 {
		places = 0
	}
	return dec.StringFixed(places)
}

// MarshalJSON renders a DecimalValue as a bare numeric JSON literal (no
// quotes, matching how int/double already render) at its own remembered
// scale, not decimal.Decimal's own default MarshalJSON (which, without
// explicitly setting the package-level decimal.MarshalJSONWithoutQuotes,
// renders as a quoted string, and strips trailing zeros regardless).
func (d DecimalValue) MarshalJSON() ([]byte, error) {
	return []byte(d.String()), nil
}

// Field is one named column of a Schema.
type Field struct {
	Name string
	Type Type
}

// Schema is the record type of a stream: an ordered list of fields. Order
// matters: it lets a sink write columns in a stable order even though
// Row.Fields is an unordered map (see jsonlSink).
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
// enforce design.md §3's rule 3, "sinks reject unmasked PII". Checking in
// declared order keeps the field named in the error message deterministic.
func (s Schema) FirstPII() (Field, bool) {
	for _, f := range s.Fields {
		if f.Type.PII {
			return f, true
		}
	}
	return Field{}, false
}

// FirstOptional returns the first field still Optional, in declared
// order, and whether one was found. It mirrors FirstPII's rule
// (design/optional-fields.md §3: "reaching a sink... mirrors the PII sink
// rule"): the checker calls this once per sink to reject a still-optional
// field the same way it rejects unmasked PII.
func (s Schema) FirstOptional() (Field, bool) {
	for _, f := range s.Fields {
		if f.Type.Optional {
			return f, true
		}
	}
	return Field{}, false
}

// String renders the schema the way design.md §3 writes it:
// { name: string, age: int }. --emit-schema (module 8) just calls this.
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
// 0-based ordinal position in the stream, and a format-specific offset
// (for CSV, the record's 1-based line number).
type Provenance struct {
	Source  string
	Ordinal int
	Offset  int
}

// Failure marks a Row that failed somewhere in the pipeline: a check
// condition that was false, or (design-errors.md's phase E3) a source
// cell that wouldn't coerce to its declared type. It never duplicates
// Provenance: the Row it rides on already carries source, ordinal, and
// offset (design-errors.md §2.1).
type Failure struct {
	Reason string // human-facing, e.g. "missing email"
	Stage  string // originating stage, e.g. "check", "csv:age"
}

// Row is one record flowing through the pipeline: named field values plus
// provenance. Fields is a map, not an ordered struct, because a stage
// like map can add or drop columns freely. Schema order lives on Schema,
// not Row.
//
// Fail is nil for a healthy row. Once set, every stage downstream must
// treat the row as opaque and pass it through untouched: Fields may be
// incomplete or suspect, so no stage may evaluate an expression against
// it (design-errors.md §2.2). Only the driver, at the end of the chain,
// disposes of a failed row per the program's error policy.
type Row struct {
	Fields map[string]any
	Prov   Provenance
	Fail   *Failure
}
