package checker

import (
	"strings"
	"testing"
)

// TestCheckRenamePreservesPositionAndType is S2-A: rename(dob:
// birth_date) renames in place, preserving type and position
// (design-improvements.md §9).
func TestCheckRenamePreservesPositionAndType(t *testing.T) {
	cp := mustCheck(t, `source in = csv("people.csv", schema: { name: string, dob: string, age: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> rename(dob: birth_date) |> out
}`)
	if len(cp.SinkSchema.Fields) != 3 {
		t.Fatalf("SinkSchema = %s, want 3 fields", cp.SinkSchema)
	}
	// Same position (index 1, where dob originally was).
	f := cp.SinkSchema.Fields[1]
	if f.Name != "birth_date" {
		t.Errorf("Fields[1].Name = %q, want %q (renamed in place)", f.Name, "birth_date")
	}
	if f.Type.Kind.String() != "string" {
		t.Errorf("Fields[1].Type = %s, want string (type preserved)", f.Type)
	}
}

// TestCheckRenamePreservesPII is S2-B: renaming a @pii column and
// writing it unmasked is still a compile error, since the tag survived
// the rename (design-improvements.md §9).
func TestCheckRenamePreservesPII(t *testing.T) {
	err := checkErr(t, `source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> rename(email: email_addr) |> out
}`)
	if !strings.Contains(err.Error(), `field "email_addr" is @pii and reaches sink`) {
		t.Errorf("error = %v, want the renamed field still flagged as unmasked PII", err)
	}
}

func TestCheckRenameOldNotInSchema(t *testing.T) {
	err := checkErr(t, `source in = csv("people.csv", schema: { name: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> rename(emial: email) |> out
}`)
	want := `column "emial" not in schema { name: string }`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to contain %q", err, want)
	}
}

func TestCheckRenameTargetCollidesWithSurvivingColumn(t *testing.T) {
	err := checkErr(t, `source in = csv("people.csv", schema: { name: string, nickname: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> rename(nickname: name) |> out
}`)
	if !strings.Contains(err.Error(), `rename target "name" collides with an existing column`) {
		t.Errorf("error = %v, want a target-collision error", err)
	}
}

func TestCheckRenameDuplicateTarget(t *testing.T) {
	err := checkErr(t, `source in = csv("people.csv", schema: { a: string, b: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> rename(a: x, b: x) |> out
}`)
	if !strings.Contains(err.Error(), `duplicate rename target "x"`) {
		t.Errorf("error = %v, want a duplicate-target error", err)
	}
}

func TestCheckRenameSameColumnTwice(t *testing.T) {
	err := checkErr(t, `source in = csv("people.csv", schema: { a: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> rename(a: x, a: y) |> out
}`)
	if !strings.Contains(err.Error(), `column "a" renamed more than once`) {
		t.Errorf("error = %v, want a renamed-more-than-once error", err)
	}
}

// TestCheckRenameSwapIsRejectedNotOrderDependent confirms rename(a: b,
// b: a) is judged against the FULL post-rename column set, not an
// order-dependent, one-pair-at-a-time mutation. a would collide with
// nothing surviving (b is also being renamed away), and neither would
// b, so this should actually succeed.
func TestCheckRenameSwapSucceeds(t *testing.T) {
	cp := mustCheck(t, `source in = csv("people.csv", schema: { a: string, b: int })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> rename(a: b, b: a) |> out
}`)
	fA, okA := cp.SinkSchema.Lookup("a")
	fB, okB := cp.SinkSchema.Lookup("b")
	if !okA || !okB {
		t.Fatalf("SinkSchema = %s, want both a and b present after the swap", cp.SinkSchema)
	}
	// a was originally string; it's now named "b" per the swap, but its
	// value type is unaffected by which column holds which name.
	if fA.Type.Kind.String() != "int" {
		t.Errorf("field \"a\" (was \"b\") type = %s, want int", fA.Type)
	}
	if fB.Type.Kind.String() != "string" {
		t.Errorf("field \"b\" (was \"a\") type = %s, want string", fB.Type)
	}
}
