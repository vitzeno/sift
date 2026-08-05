package ast

import "testing"

func TestSourceDeclFormatsShape(t *testing.T) {
	src := &SourceDecl{
		Name:   "in",
		Format: "csv",
		Path:   "export.csv",
		Schema: SchemaLit{Fields: []SchemaField{
			{Name: "dob", TypeName: "date"},
			{Name: "last_login", TypeName: "date"},
		}},
		Formats: []FieldFormat{
			{Field: "dob", Format: "02/01/2006"},
			{Field: "last_login", Format: "2006-01-02T15:04:05Z"},
		},
	}
	if len(src.Formats) != 2 {
		t.Fatalf("Formats = %d, want 2", len(src.Formats))
	}
	if src.Formats[0].Field != "dob" || src.Formats[0].Format != "02/01/2006" {
		t.Errorf("Formats[0] = %+v, want {Field: dob, Format: \"02/01/2006\"}", src.Formats[0])
	}
}
