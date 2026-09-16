package checker

import (
	"strings"
	"testing"
)

// TestCheckSelectOrder confirms select(email, name) yields output with
// columns in that order.
func TestCheckSelectOrder(t *testing.T) {
	cp := mustCheck(t, `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> select(age, name) |> out
}`)
	if len(cp.SinkSchema.Fields) != 2 || cp.SinkSchema.Fields[0].Name != "age" || cp.SinkSchema.Fields[1].Name != "name" {
		t.Errorf("SinkSchema = %s, want fields in order [age, name]", cp.SinkSchema)
	}
}

func TestCheckSelectMissingColumn(t *testing.T) {
	err := checkErr(t, `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> select(emial) |> out
}`)
	want := `column "emial" not in schema { name: string, age: int }`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to contain %q", err, want)
	}
}

func TestCheckSelectDuplicateColumn(t *testing.T) {
	err := checkErr(t, `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> select(name, name) |> out
}`)
	if !strings.Contains(err.Error(), `duplicate column "name" in select`) {
		t.Errorf("error = %v, want a duplicate-column error", err)
	}
}

// TestCheckDropRemovesColumnAndItsReferences is S1-A: drop(age) removes
// the column from output and schema; a downstream reference to .age is
// a compile error against the reduced schema.
func TestCheckDropRemovesColumnAndItsReferences(t *testing.T) {
	cp := mustCheck(t, `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> drop(age) |> out
}`)
	if len(cp.SinkSchema.Fields) != 1 || cp.SinkSchema.Fields[0].Name != "name" {
		t.Errorf("SinkSchema = %s, want just {name}", cp.SinkSchema)
	}

	err := checkErr(t, `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> drop(age) |> filter(.age >= 18) |> out
}`)
	want := `field "age" not in schema { name: string }`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to contain %q", err, want)
	}
}

func TestCheckDropMissingColumn(t *testing.T) {
	err := checkErr(t, `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> drop(ssn) |> out
}`)
	want := `column "ssn" not in schema { name: string, age: int }`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to contain %q", err, want)
	}
}

func TestCheckDropEveryColumnRejected(t *testing.T) {
	err := checkErr(t, `source in = csv("people.csv", schema: { name: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> drop(name) |> out
}`)
	if !strings.Contains(err.Error(), "drop leaves no columns") {
		t.Errorf("error = %v, want a leaves-no-columns error", err)
	}
}

// TestCheckDropSatisfiesPIIRule is S1-C: a @pii column dropped before
// the sink compiles and runs. No mask needed, since dropping the
// column already satisfies the sink rule.
func TestCheckDropSatisfiesPIIRule(t *testing.T) {
	cp := mustCheck(t, `source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> drop(email) |> out
}`)
	if _, ok := cp.SinkSchema.FirstPII(); ok {
		t.Error("SinkSchema still has a PII field after dropping it")
	}
	if len(cp.SinkSchema.Fields) != 1 || cp.SinkSchema.Fields[0].Name != "name" {
		t.Errorf("SinkSchema = %s, want just {name}", cp.SinkSchema)
	}
}

// TestCheckPipelineNamedAfterBuiltinStageRejected confirms a named
// segment can't shadow a built-in stage name.
func TestCheckPipelineNamedAfterBuiltinStageRejected(t *testing.T) {
	for _, name := range []string{"filter", "map", "check", "select", "drop"} {
		src := `pipeline ` + name + ` = filter(.x)

source in = csv("people.csv", schema: { x: bool })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> out
}`
		err := checkErr(t, src)
		want := `"` + name + `" is a built-in stage name`
		if !strings.Contains(err.Error(), want) {
			t.Errorf("pipeline named %q: error = %v, want it to contain %q", name, err, want)
		}
	}
}
