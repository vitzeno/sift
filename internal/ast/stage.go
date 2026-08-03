package ast

import "github.com/vitzeno/sift/internal/lexer"

// Stage is one element of a pipeline's `|>` chain: either a reference to
// a declared source/sink/named-pipeline by name, or one of the built-in
// stage kinds. Sealed the same way Expr is, via stageNode.
type Stage interface {
	stageNode()
}

// BuiltinStageNames is the closed set of built-in stage names
// (design-improvements.md §7): the parser uses it to dispatch in stage
// position (an identifier followed by "(" in that set is always a
// built-in, never a named segment — v0 named segments take no
// arguments); the checker uses it to reject a named pipeline segment
// that would shadow one (`pipeline select = ...` is a compile error).
// Centralized so the two agree; adding a stage later means editing this
// set once.
//
// decision: filter/map/check are included here too, even though v0
// shipped them as literal string checks in the parser with no shared
// set and no shadow-rejection in the checker. That was a latent gap:
// `pipeline filter = ...` was never rejected, and `in |> filter |> out`
// (bare, no parens) would have silently resolved to it as a NameRef
// instead of the built-in stage. Fixing it here, where the set is first
// introduced, closes that gap for the original three stages along with
// the new ones.
var BuiltinStageNames = map[string]bool{
	"filter": true,
	"map":    true,
	"check":  true,
	"select": true,
	"drop":   true,
}

// ColumnRef is a bare column-name reference — the argument form
// select/drop/rename/mask/hash/redact all take in stage position
// (design-improvements.md §5's two-form convention: a bare identifier is
// a column name; `.field` is a FieldAccess expression, valid only inside
// filter/map/check). Column-list stages take only the former, and the
// parser rejects `.field` here with a specific diagnostic rather than a
// generic parse error.
type ColumnRef struct {
	Name string
	Pos  lexer.Pos
}

// Select keeps only the named columns, in the order named — the output
// schema is the reduced, reordered input schema (design-improvements.md
// §1).
type Select struct {
	Columns []ColumnRef
	Pos     lexer.Pos
}

func (*Select) stageNode() {}

// Drop removes the named columns, keeping every other column in its
// original order (design-improvements.md §1). A @pii column dropped
// here is gone before it could ever reach a sink — a legitimate way to
// satisfy the sink rule that needs no mask/hash/redact at all.
type Drop struct {
	Columns []ColumnRef
	Pos     lexer.Pos
}

func (*Drop) stageNode() {}

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
