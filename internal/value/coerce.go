package value

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// DefaultDateFormat is the Go reference-layout used to parse a Date-kind
// cell when a source's formats: kwarg names no entry for that field
// (design/date.md §3): ISO-8601 (YYYY-MM-DD).
const DefaultDateFormat = "2006-01-02"

// DefaultDateTimeFormat is the Go reference-layout used to parse a
// DateTime-kind cell when a source's formats: kwarg names no entry for
// that field (design/datetime.md §2): ISO-8601 with a time component, no
// zone suffix, since DateTime is naive only.
const DefaultDateTimeFormat = "2006-01-02T15:04:05"

// Coerce converts a raw cell string into t's scalar kind, or returns a
// Failure if it doesn't parse (design-errors.md §2.3). Every source
// (csv, xlsx, ...) shares this one function, so a bad cell is never a
// panic.
//
// Coerce never sets Failure.Stage. The caller does that, since only it
// knows which format and column it's called for.
//
// An Optional field with a blank cell returns Absent instead of parsing
// (design/optional-fields.md §2). A missing column looks the same as a
// blank cell here: the caller passes raw = "" for both, and a missing
// required column is caught earlier as its own error. A cell that's
// present but garbage still fails, even when the field is Optional.
//
// dateFormat is the Go reference-layout to parse a Date- or DateTime-kind
// cell against; every other Kind ignores it and every non-temporal caller
// passes "". DateTime reuses this exact parameter rather than adding a
// second one (design/datetime.md §3): the caller already resolves the
// right layout string and default per field before calling Coerce, so
// Coerce itself never needs to know which of the two temporal Kinds it's
// being asked for beyond its own switch case.
func Coerce(t Type, raw string, dateFormat string) (any, *Failure) {
	if t.Optional && strings.TrimSpace(raw) == "" {
		return Absent{Kind: t.Kind}, nil
	}
	switch t.Kind {
	case String:
		return raw, nil
	case Int:
		v, err := strconv.Atoi(raw)
		if err != nil {
			return nil, &Failure{Reason: fmt.Sprintf("cannot parse %q as int", raw)}
		}
		return v, nil
	case Double:
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, &Failure{Reason: fmt.Sprintf("cannot parse %q as double", raw)}
		}
		return v, nil
	case Bool:
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, &Failure{Reason: fmt.Sprintf("cannot parse %q as bool", raw)}
		}
		return v, nil
	case Date:
		format := DefaultDateFormat
		if dateFormat != "" {
			format = dateFormat
		}
		v, err := time.Parse(format, raw)
		if err != nil {
			return nil, &Failure{Reason: fmt.Sprintf("cannot parse %q as date", raw)}
		}
		return DateValue(v), nil
	case Decimal:
		v, err := decimal.NewFromString(stripThousands(raw))
		if err != nil {
			return nil, &Failure{Reason: fmt.Sprintf("cannot parse %q as decimal", raw)}
		}
		return DecimalValue(v), nil
	case DateTime:
		format := DefaultDateTimeFormat
		if dateFormat != "" {
			format = dateFormat
		}
		v, err := time.Parse(format, raw)
		if err != nil {
			return nil, &Failure{Reason: fmt.Sprintf("cannot parse %q as datetime", raw)}
		}
		return DateTimeValue(v), nil
	default:
		return nil, &Failure{Reason: fmt.Sprintf("unsupported scalar kind %v", t.Kind)}
	}
}

// stripThousands strips a US/UK-style thousands-separator comma from a
// decimal cell before Coerce parses it (design/decimal-leniency.md §2),
// so "2,100.00" and "2,100" parse as 2100.00 and 2100 instead of failing.
// Applied unconditionally to every decimal field, no opt-in required.
//
// Commas are stripped only up to the last '.' in the cell; a comma found
// after it means this isn't thousands-then-decimal formatting at all
// (e.g. European "1.234,56"), so the whole cell is left untouched and
// falls through to decimal.NewFromString exactly as it would otherwise --
// a loud parse failure instead of a silently wrong number.
func stripThousands(raw string) string {
	dot := strings.LastIndexByte(raw, '.')
	if dot == -1 {
		return strings.ReplaceAll(raw, ",", "")
	}
	before, after := raw[:dot], raw[dot:]
	if strings.Contains(after, ",") {
		return raw
	}
	return strings.ReplaceAll(before, ",", "") + after
}
