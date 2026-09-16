package checker

import (
	"testing"

	"github.com/vitzeno/sift/internal/parser"
)

// FuzzCheck checks that any program the parser accepts, the checker either
// accepts or rejects with a diagnostic -- never a panic.
//
// The checker reserves panic for internal invariant violations ("unhandled
// expression type", "unhandled stage type", and so on). Every one of those
// is only sound if the parser can't actually produce the shape in question.
// That is an assumption about two packages agreeing, not something either
// enforces on its own, so it is worth testing against input nobody wrote by
// hand: a parser change that starts emitting a node the checker's switch
// doesn't handle turns a user's typo into a crash.
func FuzzCheck(f *testing.F) {
	seeds := []string{
		`source in = csv("p.csv", schema: { name: string, age: int })
sink out = jsonl("o.jsonl")
pipeline main { in |> filter(.age >= 18) |> out }`,
		`source in = csv("p.csv", schema: { e: string @pii })
sink out = jsonl("o.jsonl")
pipeline main { in |> map({ ...row, e: mask(.e) }) |> out }`,
		`source in = csv("p.csv", schema: { p: string? })
sink out = jsonl("o.jsonl")
pipeline main { in |> map({ ...row, p: .p ?? "n/a" }) |> out }`,
		`source in = csv("p.csv", schema: { r: string })
sink a = jsonl("a.jsonl")
sink b = jsonl("b.jsonl")
pipeline main { in |> route { .r == "EU" => a, else => b } }`,
		`pipeline gate(col, min: int) = check(.col >= min, "low")
source in = csv("p.csv", schema: { age: int })
sink out = jsonl("o.jsonl")
pipeline main { in |> gate(age, 18) |> out }`,
		`source in = csv("p.csv", schema: { d: date, x: decimal, t: datetime })
sink out = jsonl("o.jsonl")
pipeline main { in |> filter(.x > 1) |> select(d) |> out }`,
		`source in = csv("p.csv", schema: { e: string @deidentify }, key: env("K"))
sink out = jsonl("o.jsonl")
pipeline main { in |> out }`,
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, src string) {
		prog, err := parser.Parse(src)
		if err != nil {
			return // not a well-formed program; the parser's own fuzz target covers this
		}
		cp, err := Check(prog)
		if err != nil && cp != nil {
			t.Fatalf("Check returned both a program and an error: %v", err)
		}
	})
}
