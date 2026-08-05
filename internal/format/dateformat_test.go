package format

import (
	"testing"

	"github.com/vitzeno/sift/internal/value"
)

// TestResolveDateFormatEntry is DATE-B: a formats entry for the field
// drives resolution instead of the default.
func TestResolveDateFormatEntry(t *testing.T) {
	got := resolveDateFormat(map[string]string{"dob": "02/01/2006"}, "dob")
	if got != "02/01/2006" {
		t.Errorf("resolveDateFormat = %q, want %q", got, "02/01/2006")
	}
}

// TestResolveDateFormatDefault is DATE-A: a field with no formats entry
// (including a nil map, for a source with no formats kwarg at all) falls
// back to the ISO-8601 default.
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
			got := resolveDateFormat(tt.formats, "dob")
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
	if got := resolveDateFormat(formats, "dob"); got != "02/01/2006" {
		t.Errorf("resolveDateFormat(dob) = %q, want %q", got, "02/01/2006")
	}
	if got := resolveDateFormat(formats, "last_login"); got != "2006-01-02T15:04:05Z" {
		t.Errorf("resolveDateFormat(last_login) = %q, want %q", got, "2006-01-02T15:04:05Z")
	}
}
