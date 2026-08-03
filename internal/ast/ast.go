// Package ast defines Sift's abstract syntax tree: the node shapes the
// parser (module 5) builds and the checker (module 6) walks. Nodes are
// inert data — no behavior lives here — but every node carries a source
// Pos so later diagnostics can point at real code (CLAUDE.md: "every
// parser/checker error carries a source position").
package ast

import "github.com/vitzeno/sift/internal/lexer"

// Program is a whole parsed .sift file: some source declarations, some
// sink declarations, and some pipeline declarations (both the runnable
// `pipeline main { ... }` and reusable named segments like
// `pipeline clean = ...` — the AST doesn't distinguish them; that's a
// checker concern, not a syntax one).
//
// decision: no ErrorPolicy field yet. `on error abort` is v0's default
// and takes effect by doing nothing extra; `on error skip` and
// `on error |> errors` add real behavior (catch-and-continue, a second
// sink) and the latter is structurally fan-out, which design.md §5
// defers explicitly. CLAUDE.md's v0 feature list doesn't call for
// configurable error policy either. Add the node once a module actually
// consumes it.
//
// decision: no FuncDecl either. design.md §5 lists scalar user functions
// as "optional" for v0, and neither acceptance case (§7) uses one. The
// lexer already reserves `func`; the AST node can follow once something
// needs it.
type Program struct {
	Sources   []*SourceDecl
	Sinks     []*SinkDecl
	Pipelines []*PipelineDecl
	Pos       lexer.Pos
}

// SourceDecl is `source NAME = FORMAT(PATH, schema: { ... })`. Format is
// stored as the bare identifier text (e.g. "csv") — the AST never
// validates it; resolving it to a registered Source constructor is the
// checker/executor's job (CLAUDE.md non-negotiable #4).
type SourceDecl struct {
	Name   string
	Format string
	Path   string
	Schema SchemaLit
	Pos    lexer.Pos
}

// SinkDecl is `sink NAME = FORMAT(PATH)`. Unlike SourceDecl, it carries
// no schema: a sink's expected schema is whatever the checker computes
// for the stream that reaches it (design.md §3), never declared.
type SinkDecl struct {
	Name   string
	Format string
	Path   string
	Pos    lexer.Pos
}

// PipelineDecl binds Name to a `|>` chain of Stages. Modeled as a flat
// slice rather than a nested tree of binary Pipe nodes: v0 has no
// branching (no fan-out/fan-in, design.md §5), so the chain is always
// linear, and a slice matches that directly instead of adding tree
// structure nothing will ever branch.
type PipelineDecl struct {
	Name string
	Body []Stage
	Pos  lexer.Pos
}

// SchemaField is one `name: TypeName [@pii]` pair in a schema literal.
// TypeName is left as the raw identifier text (e.g. "string", "int") —
// resolving it to a value.Kind happens in the checker, not the parser,
// per design.md §4's compilation pipeline (lexer -> parser -> AST ->
// checker -> ...).
type SchemaField struct {
	Name     string
	TypeName string
	PII      bool
	Pos      lexer.Pos
}

// SchemaLit is the `{ name: string, age: int }` type literal that
// appears in a source declaration's `schema:` argument. It is a type-level
// construct, distinct from RecordExpr (a value-level record literal used
// inside map) — the two are never interchangeable.
type SchemaLit struct {
	Fields []SchemaField
	Pos    lexer.Pos
}
