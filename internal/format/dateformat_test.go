package format

import (
	"testing"

	"github.com/vitzeno/sift/internal/value"
)

// TestResolveDateFormatEntry is DATE-B: a formats entry for the field
// drives resolution instead of the default.
func TestResolveDateFormatEntry(t *testing.T) {
	got := ResolveDateFormat(map[string]string{"dob": "02/01/2006"}, "dob", value.DefaultDateFormat)
	if got != "02/01/2006" {
		t.Errorf("resolveDateFormat = %q, want %q", got, "02/01/2006")
	}
}

// TestResolveDateFormatDefault is DATE-A: a field with no formats entry
// (including a nil map, for a source with no formats kwarg at all) falls
// back to the default passed in.
func TestResolveDateFormatDefault(t *testing.T) {
	tests := []struct {
		name    string
		formats map[string]string
	}{
		{"nil map", nil},
		{"map missing this field", map[string]string{"other": "02/01/2006"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveDateFormat(tt.formats, "dob", value.DefaultDateFormat)
			if got != value.DefaultDateFormat {
				t.Errorf("resolveDateFormat = %q, want %q", got, value.DefaultDateFormat)
			}
		})
	}
}

// TestResolveDateFormatPerField is DATE-B2: two fields on the same
// formats map resolve independently.
func TestResolveDateFormatPerField(t *testing.T) {
	formats := map[string]string{
		"dob":        "02/01/2006",
		"last_login": "2006-01-02T15:04:05Z",
	}
	if got := ResolveDateFormat(formats, "dob", value.DefaultDateFormat); got != "02/01/2006" {
		t.Errorf("ResolveDateFormat(dob) = %q, want %q", got, "02/01/2006")
	}
	if got := ResolveDateFormat(formats, "last_login", value.DefaultDateFormat); got != "2006-01-02T15:04:05Z" {
		t.Errorf("ResolveDateFormat(last_login) = %q, want %q", got, "2006-01-02T15:04:05Z")
	}
}

// TestResolveDateFormatDefaultIsPerField confirms a datetime field with
// no formats entry falls back to value.DefaultDateTimeFormat, not
// value.DefaultDateFormat, on the same source as a date field that does
// fall back to the date default -- proving the two temporal Kinds don't
// share one hardcoded fallback.
func TestResolveDateFormatDefaultIsPerField(t *testing.T) {
	if got := ResolveDateFormat(nil, "dob", DefaultFormatForKind(value.Date)); got != value.DefaultDateFormat {
		t.Errorf("ResolveDateFormat(date) = %q, want %q", got, value.DefaultDateFormat)
	}
	if got := ResolveDateFormat(nil, "signup_time", DefaultFormatForKind(value.DateTime)); got != value.DefaultDateTimeFormat {
		t.Errorf("ResolveDateFormat(datetime) = %q, want %q", got, value.DefaultDateTimeFormat)
	}
}

// TestDefaultDateFormat confirms DefaultFormatForKind picks the right
// zero-config layout per Kind.
func TestDefaultDateFormat(t *testing.T) {
	if got := DefaultFormatForKind(value.Date); got != value.DefaultDateFormat {
		t.Errorf("DefaultFormatForKind(Date) = %q, want %q", got, value.DefaultDateFormat)
	}
	if got := DefaultFormatForKind(value.DateTime); got != value.DefaultDateTimeFormat {
		t.Errorf("DefaultFormatForKind(DateTime) = %q, want %q", got, value.DefaultDateTimeFormat)
	}
}
