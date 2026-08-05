package format

import (
	"testing"

	"github.com/vitzeno/sift/internal/value"
)

// TestResolveDateFormatEntry is DATE-B: a formats entry for the field
// drives resolution instead of the default.
func TestResolveDateFormatEntry(t *testing.T) {
	got := resolveDateFormat(map[string]string{"dob": "02/01/2006"}, "dob", value.DefaultDateFormat)
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
			got := resolveDateFormat(tt.formats, "dob", value.DefaultDateFormat)
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
	if got := resolveDateFormat(formats, "dob", value.DefaultDateFormat); got != "02/01/2006" {
		t.Errorf("resolveDateFormat(dob) = %q, want %q", got, "02/01/2006")
	}
	if got := resolveDateFormat(formats, "last_login", value.DefaultDateFormat); got != "2006-01-02T15:04:05Z" {
		t.Errorf("resolveDateFormat(last_login) = %q, want %q", got, "2006-01-02T15:04:05Z")
	}
}

// TestResolveDateFormatDefaultIsPerField is DT-B/DT-K's building block:
// a datetime field with no formats entry falls back to
// value.DefaultDateTimeFormat, not value.DefaultDateFormat, on the same
// source as a date field that does fall back to the date default --
// proving the two temporal Kinds don't share one hardcoded fallback
// (design/datetime.md §3).
func TestResolveDateFormatDefaultIsPerField(t *testing.T) {
	if got := resolveDateFormat(nil, "dob", defaultDateFormat(value.Date)); got != value.DefaultDateFormat {
		t.Errorf("resolveDateFormat(date) = %q, want %q", got, value.DefaultDateFormat)
	}
	if got := resolveDateFormat(nil, "signup_time", defaultDateFormat(value.DateTime)); got != value.DefaultDateTimeFormat {
		t.Errorf("resolveDateFormat(datetime) = %q, want %q", got, value.DefaultDateTimeFormat)
	}
}

// TestDefaultDateFormat confirms defaultDateFormat picks the right
// zero-config layout per Kind.
func TestDefaultDateFormat(t *testing.T) {
	if got := defaultDateFormat(value.Date); got != value.DefaultDateFormat {
		t.Errorf("defaultDateFormat(Date) = %q, want %q", got, value.DefaultDateFormat)
	}
	if got := defaultDateFormat(value.DateTime); got != value.DefaultDateTimeFormat {
		t.Errorf("defaultDateFormat(DateTime) = %q, want %q", got, value.DefaultDateTimeFormat)
	}
}
