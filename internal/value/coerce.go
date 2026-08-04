package value

import (
	"fmt"
	"strconv"
	"strings"
)

// Coerce converts a source cell's raw string into t's declared scalar
// kind, returning a Failure instead of an error when it doesn't parse —
// this is the shared helper design-errors.md's phase E3 promises: the
// one place every Source implementation converts a raw cell to a typed
// value, so a bad cell becomes a row Failure (design-errors.md §2.3)
// instead of a panic. csvSource calls it today; the xlsx/parquet
// connectors reuse it unchanged.
//
// Coerce never sets Failure.Stage: it has no notion of which format or
// column it's being called for. The caller fills that in — csvSource
// uses "csv:<field>" — since only the caller knows both.
//
// When t is Optional, a blank raw cell yields Absent rather than running
// the parse below (design/optional-fields.md §2's trichotomy). A missing
// column is indistinguishable from a blank cell here by design: the
// caller passes raw = "" for a column that isn't in the header at all,
// which lands in the same branch — a required column's absence is instead
// a structural error the caller catches once, before any row reaches
// Coerce (design/optional-fields.md §2.1). A present-but-unparseable cell
// still fails below even when t is Optional: optionality excuses absence,
// never malformed presence.
func Coerce(t Type, raw string) (any, *Failure) {
	if t.Optional && strings.TrimSpace(raw) == "" {
		return Absent{}, nil
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
	default:
		return nil, &Failure{Reason: fmt.Sprintf("unsupported scalar kind %v", t.Kind)}
	}
}
