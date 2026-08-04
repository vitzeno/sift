package value

import (
	"fmt"
	"strconv"
	"strings"
)

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
