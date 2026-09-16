package parser

import "testing"

// FuzzParse checks that Parse never panics and never returns both a program
// and an error. The parser bails out of its ~20 mutually recursive parse*
// functions by panicking with a *ParseError and recovering at the entry
// point; a panic that isn't one of those, or one raised after the recover
// is set up but outside its reach, would escape to the caller as a crash
// instead of a diagnostic. Malformed input is a normal thing for a compiler
// to be handed, so that must never happen.
func FuzzParse(f *testing.F) {
	seeds := []string{
		`source in = csv("p.csv", schema: { name: string, age: int })
sink out = jsonl("o.jsonl")
pipeline main { in |> filter(.age >= 18) |> out }`,
		`pipeline clean = check(.email != "", "missing") |> map({ ...row, e: lower(.e) })`,
		`on error |> errs`,
		`pipeline main { in |> route { .r == "EU" => a, else => discard } }`,
		`source in = csv("p.csv", schema: { d: date, x: decimal, t: datetime },
  columns: { d: "The Date" }, formats: { d: "02/01/2006" })`,
		`source in = csv("p.csv", schema: { e: string @deidentify }, key: env("K"))`,
		`pipeline gate(col, min: int) = check(.col >= min, "low")`,
		`map({ ...row, a: .b ?? 0 })`,
		"",
		"\x00\xff",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, src string) {
		prog, err := Parse(src)
		if err != nil && prog != nil {
			t.Fatalf("Parse returned both a program and an error: %v", err)
		}
		if err == nil && prog == nil {
			t.Fatal("Parse returned neither a program nor an error")
		}
	})
}
