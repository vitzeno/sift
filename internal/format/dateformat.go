package format

import "github.com/vitzeno/sift/internal/value"

// resolveDateFormat returns the Go reference-layout to parse field's
// cells against (design/date.md §3): formats[field], if present, else
// value.DefaultDateFormat. csvSource and xlsxSource share this one
// function rather than each falling back their own way.
func resolveDateFormat(formats map[string]string, field string) string {
	if f, ok := formats[field]; ok {
		return f
	}
	return value.DefaultDateFormat
}
