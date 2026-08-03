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
	"rename": true,
	"limit":  true,
	"offset": true,
	"mask":   true,
	"hash":   true,
	"redact": true,
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

// RenamePair is one `old: new` pair in a rename stage's argument list —
// old must be a column already in the input schema, new is the column's
// replacement name.
type RenamePair struct {
	Old string
	New string
	Pos lexer.Pos
}

// Rename renames each pair's Old column to New in place: same position,
// same exact type — including the @pii tag (design-improvements.md §2).
// Preserving the tag is load-bearing: renaming a PII column must not be
// a way to launder it.
type Rename struct {
	Pairs []RenamePair
	Pos   lexer.Pos
}

func (*Rename) stageNode() {}

// Limit emits at most N rows then stops; schema passes through
// unchanged. N is always a non-negative compile-time constant — v0's
// grammar has no unary minus, so a negative int literal can't even be
// written (design-improvements.md §3).
//
// N counts every row Limit sees, healthy or failed
// (design-improvements.md §3's positional-over-all-rows rule): Limit
// has no way to know the active error policy, so it can't know whether
// a failed row will later be skipped, routed, or abort the run — it
// just counts what it itself emits.
type Limit struct {
	N   int64
	Pos lexer.Pos
}

func (*Limit) stageNode() {}

// Offset discards the first N rows pulled from upstream, then passes
// the rest through unchanged; schema passes through unchanged. Like
// Limit, N counts every row including failed ones.
type Offset struct {
	N   int64
	Pos lexer.Pos
}

func (*Offset) stageNode() {}

// Declassify applies Fn (mask, hash, or redact) to each named column in
// place, clearing its @pii tag (design-improvements.md §4). It's one of
// only two ways to clear the tag — dropping the column (§1) is the
// other. Fn and Columns share the same column-list shape select/drop
// use; unlike them, every named column must already be string @pii.
//
// This is the stage-position twin of the same-named expression
// function (design-improvements.md §6): `|> mask(email)` here,
// `map({ id: mask(.email) })` there, both backed by one shared
// implementation.
type Declassify struct {
	Fn      string
	Columns []ColumnRef
	Pos     lexer.Pos
}

func (*Declassify) stageNode() {}

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
