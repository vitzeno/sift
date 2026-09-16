package parser

import (
	"strings"
	"testing"
)

func TestParseSourceFormats(t *testing.T) {
	const src = `source in = csv("export.csv",
  schema:  { dob: date, last_login: date },
  formats: { dob: "02/01/2006", last_login: "2006-01-02T15:04:05Z" }
)`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	s := prog.Sources[0]
	if len(s.Formats) != 2 {
		t.Fatalf("Formats = %d, want 2", len(s.Formats))
	}
	if s.Formats[0].Field != "dob" || s.Formats[0].Format != "02/01/2006" {
		t.Errorf("Formats[0] = %+v, want {Field: dob, Format: \"02/01/2006\"}", s.Formats[0])
	}
	if s.Formats[1].Field != "last_login" || s.Formats[1].Format != "2006-01-02T15:04:05Z" {
		t.Errorf("Formats[1] = %+v, want {Field: last_login, Format: \"2006-01-02T15:04:05Z\"}", s.Formats[1])
	}
}

// TestParseSourceFormatsIsOptional confirms a source with no formats
// kwarg at all still parses fine, unaffected by this feature existing.
func TestParseSourceFormatsIsOptional(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string })`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if prog.Sources[0].Formats != nil {
		t.Errorf("Formats = %+v, want nil", prog.Sources[0].Formats)
	}
}

func TestParseSourceDuplicateFormats(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { dob: date }, formats: { dob: "2006-01-02" }, formats: { dob: "02/01/2006" })`
	_, err := Parse(src)
	if err == nil {
		t.Fatal("Parse error = nil, want a duplicate-formats error")
	}
	if got := err.Error(); !strings.Contains(got, `duplicate "formats"`) {
		t.Errorf("error = %q, want it to mention the duplicate formats kwarg", got)
	}
}

// TestParseSourceColumnsAndFormatsTogether confirms the two per-field
// kwargs coexist without interfering.
func TestParseSourceColumnsAndFormatsTogether(t *testing.T) {
	const src = `source in = csv("export.csv",
  schema:  { dob: date },
  columns: { dob: "Date of Birth" },
  formats: { dob: "02/01/2006" }
)`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	s := prog.Sources[0]
	if len(s.Columns) != 1 || s.Columns[0].Header != "Date of Birth" {
		t.Errorf("Columns = %+v, want one entry aliasing to \"Date of Birth\"", s.Columns)
	}
	if len(s.Formats) != 1 || s.Formats[0].Format != "02/01/2006" {
		t.Errorf("Formats = %+v, want one entry with format \"02/01/2006\"", s.Formats)
	}
}
