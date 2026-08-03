package checker

import "testing"

func TestCheckLimitOffsetSchemaUnchanged(t *testing.T) {
	cp := mustCheck(t, `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> limit(10) |> offset(2) |> out
}`)
	if len(cp.SinkSchema.Fields) != 2 || cp.SinkSchema.Fields[0].Name != "name" || cp.SinkSchema.Fields[1].Name != "age" {
		t.Errorf("SinkSchema = %s, want it unchanged from the source schema", cp.SinkSchema)
	}
	if len(cp.Stages) != 2 {
		t.Fatalf("Stages = %d, want 2 (limit, offset)", len(cp.Stages))
	}
}
