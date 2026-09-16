package checker

import (
	"strings"
	"testing"
)

// TestCheckSourceSchemaDeidentified confirms a @deidentify field
// resolves to the wrapping value.Deidentified type, not a tag alongside
// string the way @pii is.
func TestCheckSourceSchemaDeidentified(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, email: string @deidentify }, key: env("SIFT_KEY"))
sink out = jsonl("out.jsonl")

pipeline main {
  in |> out
}`
	cp := mustCheck(t, src)
	want := "{ name: string, email: deidentified<string> }"
	if got := cp.SourceSchema.String(); got != want {
		t.Errorf("SourceSchema = %s, want %s", got, want)
	}
}

// TestCheckSourceSchemaDeidentifiedOptionalDischargesAtIngest confirms
// `string? @deidentify` still requires key:, but the field's own
// Optional flag never reaches the outer type -- only
// Coerce needs to know it was declared optional, to pick the absence
// marker over a real cell at ingest.
func TestCheckSourceSchemaDeidentifiedOptionalDischargesAtIngest(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, phone: string? @deidentify }, key: env("SIFT_KEY"))
sink out = jsonl("out.jsonl")

pipeline main {
  in |> out
}`
	cp := mustCheck(t, src)
	f, ok := cp.SourceSchema.Lookup("phone")
	if !ok {
		t.Fatal("phone not in source schema")
	}
	if f.Type.Optional {
		t.Errorf("phone.Type.Optional = true, want false: optionality must be discharged at ingest, never reach a stage")
	}
	if _, ok := cp.SourceSchema.FirstOptional(); ok {
		t.Error("FirstOptional found phone, want optionality already discharged")
	}
}

// TestCheckDeidentifyAndPIIConflict confirms the two tags describe
// incompatible handling of the same column and are rejected together,
// regardless of order.
func TestCheckDeidentifyAndPIIConflict(t *testing.T) {
	for _, src := range []string{
		`source in = csv("people.csv", schema: { email: string @pii @deidentify }, key: env("SIFT_KEY"))
sink out = jsonl("out.jsonl")
pipeline main { in |> out }`,
		`source in = csv("people.csv", schema: { email: string @deidentify @pii }, key: env("SIFT_KEY"))
sink out = jsonl("out.jsonl")
pipeline main { in |> out }`,
	} {
		err := checkErr(t, src)
		want := `field "email" cannot be both @pii and @deidentify`
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to contain %q", err, want)
		}
	}
}

// TestCheckDeidentifySinkAcceptsUnconditionally confirms that unlike
// @pii, reaching a sink is the whole point and never an error -- there
// is no declassifier to call first.
func TestCheckDeidentifySinkAcceptsUnconditionally(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, email: string @deidentify }, key: env("SIFT_KEY"))
sink out = jsonl("out.jsonl")

pipeline main {
  in |> out
}`
	cp := mustCheck(t, src)
	want := "{ name: string, email: deidentified<string> }"
	if got := cp.SinkSchema.String(); got != want {
		t.Errorf("SinkSchema = %s, want %s", got, want)
	}
}

// TestCheckDeidentifyRejectsFieldAccess confirms `.col` is rejected in
// any expression, exercised through filter, which every other rejection
// (comparison, function argument, segment body) shares the same
// checkExpr code path with.
func TestCheckDeidentifyRejectsFieldAccess(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, email: string @deidentify }, key: env("SIFT_KEY"))
sink out = jsonl("out.jsonl")

pipeline main {
  in |> filter(.email == "x") |> out
}`
	err := checkErr(t, src)
	want := `field "email" is @deidentify and cannot be used in an expression`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to contain %q", err, want)
	}
}

// TestCheckDeidentifyRejectsComparisonBetweenTwoDeidentifiedColumns
// confirms comparison is rejected even against another deidentified
// field: both sides share Kind == Deidentified, so the
// ordinary Kind-equality check that EQ/NE otherwise pass would let this
// through if field access weren't already blocked first.
func TestCheckDeidentifyRejectsComparisonBetweenTwoDeidentifiedColumns(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { email1: string @deidentify, email2: string @deidentify }, key: env("SIFT_KEY"))
sink out = jsonl("out.jsonl")

pipeline main {
  in |> filter(.email1 == .email2) |> out
}`
	err := checkErr(t, src)
	want := `field "email1" is @deidentify`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to contain %q", err, want)
	}
}

// TestCheckDeidentifyRejectsDeclassifier confirms mask/hash/redact are
// rejected: there is nothing to declassify, since the type is not @pii.
// Uses the stage
// form (`hash(email)`, mirroring TestCheckDeclassifyingSegmentOnNonPIIColumnRejected)
// rather than map's expression form, since map's own assignment rule
// would otherwise block this on "cannot assign" first and never reach
// checkDeclassify's own rejection.
func TestCheckDeidentifyRejectsDeclassifier(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, email: string @deidentify }, key: env("SIFT_KEY"))
sink out = jsonl("out.jsonl")

pipeline main {
  in |> hash(email) |> out
}`
	err := checkErr(t, src)
	want := `hash on non-PII column "email"`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to contain %q", err, want)
	}
}

// TestCheckDeidentifyRejectsCoalesce confirms ?? is rejected: there is
// nothing to discharge, since optionality is resolved at ingest. Assigns
// to a new field
// name (not "email") so map's own assignment rule doesn't block this on
// "cannot assign" before checkExpr ever evaluates the ?? and reads .email.
func TestCheckDeidentifyRejectsCoalesce(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, email: string @deidentify }, key: env("SIFT_KEY"))
sink out = jsonl("out.jsonl")

pipeline main {
  in |> map({ ...row, contact: .email ?? "n/a" }) |> out
}`
	err := checkErr(t, src)
	want := `field "email" is @deidentify`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to contain %q", err, want)
	}
}

// TestCheckDeidentifyRejectsMapAssignment covers assignment in
// map({ ...row, col: ... }), the one rejection that field-access
// blocking alone can't cover: the RHS here never reads .email back, so
// only an explicit check on the assignment target catches it.
func TestCheckDeidentifyRejectsMapAssignment(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, email: string @deidentify }, key: env("SIFT_KEY"))
sink out = jsonl("out.jsonl")

pipeline main {
  in |> map({ ...row, email: "redacted" }) |> out
}`
	err := checkErr(t, src)
	want := `cannot assign to "email": field is @deidentify and may not be modified`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to contain %q", err, want)
	}
}

// TestCheckDeidentifyAllowsStructuralOps confirms the structural
// operations are allowed: passthrough, select, drop, and rename all
// compile and preserve the Deidentified type.
func TestCheckDeidentifyAllowsStructuralOps(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, email: string @deidentify, note: string }, key: env("SIFT_KEY"))
sink out = jsonl("out.jsonl")

pipeline main {
  in |> select(name, email) |> rename(email: e) |> out
}`
	cp := mustCheck(t, src)
	want := "{ name: string, e: deidentified<string> }"
	if got := cp.SinkSchema.String(); got != want {
		t.Errorf("SinkSchema = %s, want %s", got, want)
	}
}

// TestCheckDeidentifyRenamedFieldStillRejectsOperations confirms a
// renamed deidentified column is still deidentified under its new name,
// not a plain string that happened to survive rename unexamined.
func TestCheckDeidentifyRenamedFieldStillRejectsOperations(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, email: string @deidentify }, key: env("SIFT_KEY"))
sink out = jsonl("out.jsonl")

pipeline main {
  in |> rename(email: e) |> filter(.e == "x") |> out
}`
	err := checkErr(t, src)
	want := `field "e" is @deidentify`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to contain %q", err, want)
	}
}

// TestCheckDeidentifyDropAllowsSinkWithoutTheColumn confirms drop is
// always available to discard a deidentified column's ciphertext
// entirely.
func TestCheckDeidentifyDropAllowsSinkWithoutTheColumn(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, email: string @deidentify }, key: env("SIFT_KEY"))
sink out = jsonl("out.jsonl")

pipeline main {
  in |> drop(email) |> out
}`
	cp := mustCheck(t, src)
	want := "{ name: string }"
	if got := cp.SinkSchema.String(); got != want {
		t.Errorf("SinkSchema = %s, want %s", got, want)
	}
}

// TestCheckDeidentifySegmentColumnParameterRejected confirms a
// deidentified column bound to a segment's column parameter is rejected
// at the instantiation site: it falls through to the same field-access
// rejection once the substituted body reads it, with the usual
// dual-site context on top -- no bespoke segment-level rule needed.
func TestCheckDeidentifySegmentColumnParameterRejected(t *testing.T) {
	const src = `pipeline scrub(col) = filter(.col == "x")

source in = csv("people.csv", schema: { name: string, email: string @deidentify }, key: env("SIFT_KEY"))
sink out = jsonl("out.jsonl")

pipeline main {
  in |> scrub(email) |> out
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `field "email" is @deidentify`) {
		t.Errorf("error = %v, want the field-access rejection", err)
	}
	if !strings.Contains(err.Error(), `in segment scrub(col = email)`) {
		t.Errorf("error = %v, want dual-site context naming the segment and binding", err)
	}
}
