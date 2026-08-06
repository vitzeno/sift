package format

import "github.com/vitzeno/sift/internal/value"

// ResolveColumns maps each schema field to a column index
// (design/column-aliases.md §3): aliases[field], if present, is the raw
// header string to match instead of field's own identifier text. A blank
// header cell (xlsx's merged-cell quirk) is never a legal match target.
// The csv and xlsx source packages share this one function rather than
// each matching columns their own way.
//
// A field that resolves neither way is simply left out of the returned
// map when it's Optional (design/optional-fields.md): Coerce already
// turns a missing index into Absent, so there's nothing more to do here.
// A required field that resolves neither way is reported back as
// missingField with ok=false; the caller builds its own error message,
// since csv and xlsx report a missing column in different shapes (xlsx
// names the sheet and header row, csv doesn't).
func ResolveColumns(schema value.Schema, headers []string, aliases map[string]string) (resolved map[string]int, missingField string, ok bool) {
	col := make(map[string]int, len(headers))
	for i, h := range headers {
		if h == "" {
			continue
		}
		col[h] = i
	}

	resolved = make(map[string]int, len(schema.Fields))
	for _, field := range schema.Fields {
		want := field.Name
		if alias, aliased := aliases[field.Name]; aliased {
			want = alias
		}
		if idx, found := col[want]; found {
			resolved[field.Name] = idx
			continue
		}
		if field.Type.Optional {
			continue
		}
		return nil, field.Name, false
	}
	return resolved, "", true
}
