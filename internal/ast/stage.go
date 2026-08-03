package ast

import "github.com/vitzeno/sift/internal/lexer"

// Stage is one element of a pipeline's `|>` chain: either a reference to
// a declared source/sink/named-pipeline by name, or one of v0's three
// built-in stage kinds. Sealed the same way Expr is, via stageNode.
type Stage interface {
	stageNode()
}

// NameRef refers to a declared source, sink, or named pipeline segment
// by name — the "in" and "out" in `in |> filter(...) |> out`, or a named
// segment like "clean" in `in |> clean |> out`. Which namespace it
// resolves into is the checker's job, not the parser's.
type NameRef struct {
	Name string
	Pos  lexer.Pos
}

func (*NameRef) stageNode() {}

// Filter keeps rows where Pred evaluates true; schema passes through
// unchanged (design.md §3).
type Filter struct {
	Pred Expr
	Pos  lexer.Pos
}

func (*Filter) stageNode() {}

// Map rebuilds each row from Record; the output schema is recomputed
// from Record's fields (design.md §3), never declared.
type Map struct {
	Record *RecordExpr
	Pos    lexer.Pos
}

func (*Map) stageNode() {}

// Check fails a row (routed per the error policy) when Cond is false,
// citing Reason. Schema passes through unchanged, same as Filter.
type Check struct {
	Cond   Expr
	Reason string
	Pos    lexer.Pos
}

func (*Check) stageNode() {}
