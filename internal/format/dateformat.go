package format

import "github.com/vitzeno/sift/internal/value"

// ResolveDateFormat returns the Go reference-layout to parse field's
// cells against (design/date.md §3, widened by design/datetime.md §3):
// formats[field], if present, else defaultFormat. The csv and xlsx
// source packages share this one function rather than each falling back
// their own way; DefaultFormatForKind picks defaultFormat per the
// field's own Kind, since Date and DateTime don't share a zero-config
// layout.
func ResolveDateFormat(formats map[string]string, field, defaultFormat string) string {
	if f, ok := formats[field]; ok {
		return f
	}
	return defaultFormat
}

// DefaultFormatForKind returns the zero-config layout Coerce falls back
// to when a source's formats: kwarg names no entry for a field, chosen by
// the field's own Kind (design/datetime.md §3): value.DefaultDateFormat
// for Date, value.DefaultDateTimeFormat for DateTime. Every other Kind
// ignores Coerce's dateFormat parameter entirely, so its return value
// here is never actually read for them; Date's default is returned as a
// harmless placeholder in that case.
func DefaultFormatForKind(kind value.Kind) string {
	if kind == value.DateTime {
		return value.DefaultDateTimeFormat
	}
	return value.DefaultDateFormat
}
