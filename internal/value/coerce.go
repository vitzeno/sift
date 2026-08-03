package value

import (
	"fmt"
	"strconv"
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
func Coerce(t Type, raw string) (any, *Failure) {
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
