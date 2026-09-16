package checker

import (
	"strings"
	"testing"
)

// TestCheckDeclassifyStageClearsPII confirms |> hash(email) |> out
// compiles and runs, and that the output column is present and clean
// (string, no tag).
func TestCheckDeclassifyStageClearsPII(t *testing.T) {
	cp := mustCheck(t, `source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> hash(email) |> out
}`)
	f, ok := cp.SinkSchema.Lookup("email")
	if !ok {
		t.Fatal("email field missing from SinkSchema")
	}
	if f.Type.PII {
		t.Error("email field is still tagged PII after hash()")
	}
	if f.Type.Kind.String() != "string" {
		t.Errorf("email field type = %s, want string", f.Type)
	}
}

func TestCheckDeclassifyMaskAndRedact(t *testing.T) {
	for _, fn := range []string{"mask", "redact"} {
		cp := mustCheck(t, `source in = csv("people.csv", schema: { email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> `+fn+`(email) |> out
}`)
		if _, ok := cp.SinkSchema.FirstPII(); ok {
			t.Errorf("%s: SinkSchema still has a PII field", fn)
		}
	}
}

// TestCheckDeclassifyNonPIITargetRejected confirms redact(age) where age
// is int is a compile error with a clear message.
func TestCheckDeclassifyNonPIITargetRejected(t *testing.T) {
	err := checkErr(t, `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> redact(age) |> out
}`)
	if !strings.Contains(err.Error(), `redact on non-PII column "age"`) {
		t.Errorf("error = %v, want a non-PII-target rejection", err)
	}
}

// TestCheckDeclassifyPlainStringRejected confirms the non-PII rejection
// also fires for a plain (untagged) string column, not just a wrong
// Kind entirely: "must already be string @pii" excludes both cases.
func TestCheckDeclassifyPlainStringRejected(t *testing.T) {
	err := checkErr(t, `source in = csv("people.csv", schema: { name: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> mask(name) |> out
}`)
	if !strings.Contains(err.Error(), `mask on non-PII column "name"`) {
		t.Errorf("error = %v, want a non-PII-target rejection", err)
	}
}

func TestCheckDeclassifyMissingColumn(t *testing.T) {
	err := checkErr(t, `source in = csv("people.csv", schema: { name: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> mask(emial) |> out
}`)
	want := `column "emial" not in schema { name: string }`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to contain %q", err, want)
	}
}

func TestCheckDeclassifyDuplicateColumn(t *testing.T) {
	err := checkErr(t, `source in = csv("people.csv", schema: { email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> mask(email, email) |> out
}`)
	if !strings.Contains(err.Error(), `duplicate column "email" in mask`) {
		t.Errorf("error = %v, want a duplicate-column error", err)
	}
}

// TestCheckDeclassifyLeavesOtherColumnsUnchanged confirms declassifying
// one column doesn't disturb the rest of the schema.
func TestCheckDeclassifyLeavesOtherColumnsUnchanged(t *testing.T) {
	cp := mustCheck(t, `source in = csv("people.csv", schema: { name: string, age: int, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> mask(email) |> out
}`)
	if len(cp.SinkSchema.Fields) != 3 {
		t.Fatalf("SinkSchema = %s, want 3 fields", cp.SinkSchema)
	}
	if cp.SinkSchema.Fields[0].Name != "name" || cp.SinkSchema.Fields[1].Name != "age" {
		t.Errorf("SinkSchema = %s, want name and age untouched, in original order", cp.SinkSchema)
	}
}
