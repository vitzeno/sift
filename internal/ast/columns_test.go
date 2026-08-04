package ast

import "testing"

func TestSourceDeclColumnsShape(t *testing.T) {
	src := &SourceDecl{
		Name:   "in",
		Format: "csv",
		Path:   "transactions.csv",
		Schema: SchemaLit{Fields: []SchemaField{
			{Name: "txn_id", TypeName: "int"},
			{Name: "txn_date", TypeName: "string"},
		}},
		Columns: []ColumnAlias{
			{Field: "txn_id", Header: "Transaction ID"},
			{Field: "txn_date", Header: "Date"},
		},
	}
	if len(src.Columns) != 2 {
		t.Fatalf("Columns = %d, want 2", len(src.Columns))
	}
	if src.Columns[0].Field != "txn_id" || src.Columns[0].Header != "Transaction ID" {
		t.Errorf("Columns[0] = %+v, want {Field: txn_id, Header: \"Transaction ID\"}", src.Columns[0])
	}
}
