// Package ast defines Sift's abstract syntax tree: the node shapes the
// parser (module 5) builds and the checker (module 6) walks. Nodes are
// inert data, no behavior lives here, but every node carries a source
// Pos so diagnostics can point at real code (CLAUDE.md: "every
// parser/checker error carries a source position").
package ast

import "github.com/vitzeno/sift/internal/lexer"

// Program is a whole parsed .sift file: some source declarations, some
// sink declarations, some pipeline declarations (both the runnable
// `pipeline main { ... }` and reusable named segments like
// `pipeline clean = ...`, which the AST doesn't distinguish since that's
// a checker concern, not a syntax one), and at most one error policy.
//
// decision: no FuncDecl. design.md §5 lists scalar user functions as
// "optional" for v0, and neither acceptance case (§7) uses one. The
// lexer already reserves `func`; the AST node can follow once something
// needs it.
type Program struct {
	Sources     []*SourceDecl
	Sinks       []*SinkDecl
	Pipelines   []*PipelineDecl
	ErrorPolicy *ErrorPolicyDecl // nil = absent; the checker defaults to Abort
	Pos         lexer.Pos
}

// ErrorPolicyKind classifies an ErrorPolicyDecl (design-errors.md §3.1).
type ErrorPolicyKind int

const (
	ErrorAbort ErrorPolicyKind = iota
	ErrorSkip
	ErrorRoute
)

// ErrorPolicyDecl is `on error (abort | skip | |> <name>)`
// (design-errors.md §5, reintroducing what v0 deferred). Target is only
// set when Kind == ErrorRoute, and names a sink by the same NameRef path
// a pipeline body's stage-chain NameRefs already use. Resolving it to
// an actual sink declaration is the checker's job, not the parser's.
type ErrorPolicyDecl struct {
	Kind   ErrorPolicyKind
	Target *NameRef
	Pos    lexer.Pos
}

// SourceDecl is `source NAME = FORMAT(PATH, schema: { ... }, ...)`. Format
// is stored as the bare identifier text (e.g. "csv"). The AST never
// validates it; resolving it to a registered Source constructor is the
// checker/executor's job (CLAUDE.md non-negotiable #4).
//
// Opts carries every keyword argument other than the required `schema:`
// (e.g. xlsx's `sheet: "Q1"`, `header_row: 3`, design/xlsx.md §1). The
// parser and checker never interpret these, only pass them through.
// Only the registered constructor for Format knows what a given opt
// name means or validates its type, which keeps the "no format
// special-cased in the frontend" rule (CLAUDE.md non-negotiable #2)
// intact even as formats grow options csv/jsonl never needed.
//
// Columns carries the optional `columns: { ... }` kwarg
// (design/column-aliases.md §3): it's parsed the same way Schema is, as
// its own dedicated field rather than folded into Opts, since its value
// is a map of pairs, not a single scalar literal like every Opts value
// is. Like Opts, the checker never interprets it; only the registered
// Source constructor does, at construction time, against the real file
// header.
type SourceDecl struct {
	Name    string
	Format  string
	Path    string
	Schema  SchemaLit
	Columns []ColumnAlias
	Opts    []SourceOpt
	Pos     lexer.Pos
}

// SourceOpt is one keyword argument in a source declaration beyond
// `schema:`, e.g. `sheet: "Q1"` or `header_row: 3`. Value is always a
// compile-time scalar literal (IntLit/DoubleLit/StringLit/BoolLit),
// never a general expression: like a segment call's scalar argument
// (design/segments.md §2.2), there is no row in scope yet at a source
// declaration.
type SourceOpt struct {
	Name  string
	Value Expr
	Pos   lexer.Pos
}

// ColumnAlias is one `field: "Raw Header Text"` pair inside a source's
// `columns:` kwarg (design/column-aliases.md §3): Field names a schema
// field, Header is the literal header string to match against instead of
// Field's own identifier text. This is how a column whose real header
// contains spaces (or otherwise isn't a valid identifier) gets named in a
// schema at all.
type ColumnAlias struct {
	Field  string
	Header string
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
//
// Params is nil for a parameterless segment or the runnable pipeline
// (unchanged from v0). Non-empty, it turns Body into a template the
// checker specializes at each call site (design/segments.md §3) rather
// than a stage list it inlines as-is.
type PipelineDecl struct {
	Name   string
	Params []Param
	Body   []Stage
	Pos    lexer.Pos
}

// ParamKind distinguishes a named segment's two parameter kinds
// (design/segments.md §2): exactly two, no others planned.
type ParamKind int

const (
	// ParamColumn is a bare-identifier parameter, referenced `.name`
	// inside the body and passed a bare column name at the call site
	// (design/segments.md §2.1).
	ParamColumn ParamKind = iota
	// ParamScalar is a `name: type` parameter, referenced as a bare
	// value inside the body and passed a literal of that type at the
	// call site (design/segments.md §2.2).
	ParamScalar
)

// Param is one entry in a `pipeline name(params) = body` declaration's
// parameter list. TypeName is only set when Kind == ParamScalar, and is
// left as raw identifier text. Like SchemaField.TypeName, resolving it
// to a value.Kind is the checker's job, not the parser's.
type Param struct {
	Name     string
	Kind     ParamKind
	TypeName string
	Pos      lexer.Pos
}

// SchemaField is one `name: TypeName ["?"] [@pii]` pair in a schema
// literal. TypeName is left as the raw identifier text (e.g. "string",
// "int"); resolving it to a value.Kind happens in the checker, not the
// parser, per design.md §4's compilation pipeline (lexer -> parser -> AST
// -> checker -> ...). Optional marks a `?` suffix (design/optional-fields.md
// §2): the field may be absent, independent of the PII tag
// (design/optional-fields.md §4).
type SchemaField struct {
	Name     string
	TypeName string
	Optional bool
	PII      bool
	Pos      lexer.Pos
}

// SchemaLit is the `{ name: string, age: int }` type literal that
// appears in a source declaration's `schema:` argument. It is a type-level
// construct, distinct from RecordExpr (a value-level record literal used
// inside map). The two are never interchangeable.
type SchemaLit struct {
	Fields []SchemaField
	Pos    lexer.Pos
}
