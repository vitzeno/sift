// Package ast defines Sift's abstract syntax tree: the node shapes the
// parser builds and the checker walks. Nodes are inert data, no behavior
// lives here, but every node carries a source Pos so diagnostics can
// point at real code.
package ast

import "github.com/vitzeno/sift/internal/lexer"

// Program is a whole parsed .sift file: some source declarations, some
// sink declarations, some pipeline declarations (both the runnable
// `pipeline main { ... }` and reusable named segments like
// `pipeline clean = ...`, which the AST doesn't distinguish since that's
// a checker concern, not a syntax one), and at most one error policy.
//
// There is deliberately no FuncDecl: user-defined scalar functions aren't
// part of the language yet. The lexer already reserves `func`, so the AST
// node can follow once something needs it.
type Program struct {
	Sources     []*SourceDecl
	Sinks       []*SinkDecl
	Pipelines   []*PipelineDecl
	ErrorPolicy *ErrorPolicyDecl // nil = absent; the checker defaults to Abort
	Pos         lexer.Pos
}

// ErrorPolicyKind classifies an ErrorPolicyDecl.
type ErrorPolicyKind int

const (
	ErrorAbort ErrorPolicyKind = iota
	ErrorSkip
	ErrorRoute
)

// ErrorPolicyDecl is `on error (abort | skip | |> <name>)`. Target is
// only set when Kind == ErrorRoute, and names a sink by the same NameRef
// path
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
// checker/executor's job.
//
// Opts carries every keyword argument other than the required `schema:`
// (e.g. xlsx's `sheet: "Q1"`, `header_row: 3`). The parser and checker
// never interpret these, only pass them through. Only the registered
// constructor for Format knows what a given opt name means or validates
// its type, which keeps formats out of the frontend entirely even as
// they grow options csv/jsonl never needed.
//
// Columns carries the optional `columns: { ... }` kwarg: it's parsed the
// same way Schema is, as its own dedicated field rather than folded into
// Opts, since its value
// is a map of pairs, not a single scalar literal like every Opts value
// is. Like Opts, the checker never interprets it; only the registered
// Source constructor does, at construction time, against the real file
// header.
//
// Formats carries the optional `formats: { ... }` kwarg, a direct
// syntactic copy of Columns, mapping a `date`- or `datetime`-typed
// schema field to the Go reference-layout string to parse
// its cells against, instead of the ISO-8601 default. Like Columns, the
// checker never interprets it; only the registered Source constructor
// does, at construction time.
//
// Key carries the optional `key: env("NAME")` kwarg, nil for a source
// with no key kwarg. Its own EnvRef.Var isn't resolved against the
// environment until the registered Source constructor runs: the checker
// only checks that key is present when the schema has a @deidentify
// field, never the
// environment itself, so --emit-ast/--emit-schema work on a machine that
// doesn't hold the key at all.
type SourceDecl struct {
	Name    string
	Format  string
	Path    string
	Schema  SchemaLit
	Columns []ColumnAlias
	Formats []FieldFormat
	Key     *EnvRef
	Opts    []SourceOpt
	Pos     lexer.Pos
}

// EnvRef is `env("NAME")`, the only form the key: kwarg accepts. It is
// not a general expression -- there is
// no other function call syntax anywhere in a source declaration -- so
// it gets its own narrow AST node instead of reusing Call.
type EnvRef struct {
	Var string
	Pos lexer.Pos
}

// SourceOpt is one keyword argument in a source declaration beyond
// `schema:`, e.g. `sheet: "Q1"` or `header_row: 3`. Value is always a
// compile-time scalar literal (IntLit/DoubleLit/StringLit/BoolLit),
// never a general expression: like a segment call's scalar argument,
// there is no row in scope yet at a source declaration.
type SourceOpt struct {
	Name  string
	Value Expr
	Pos   lexer.Pos
}

// ColumnAlias is one `field: "Raw Header Text"` pair inside a source's
// `columns:` kwarg. Field names a schema field, Header is the literal
// header string to match against instead of
// Field's own identifier text. This is how a column whose real header
// contains spaces (or otherwise isn't a valid identifier) gets named in a
// schema at all.
type ColumnAlias struct {
	Field  string
	Header string
	Pos    lexer.Pos
}

// FieldFormat is one `field: "layout string"` pair inside a source's
// `formats:` kwarg. Field names a `date`- or `datetime`-typed schema
// field, Format is the Go reference-layout string to parse its cells
// against.
type FieldFormat struct {
	Field  string
	Format string
	Pos    lexer.Pos
}

// SinkDecl is `sink NAME = FORMAT(PATH)`. Unlike SourceDecl, it carries
// no schema: a sink's expected schema is whatever the checker computes
// for the stream that reaches it, never declared.
type SinkDecl struct {
	Name   string
	Format string
	Path   string
	Pos    lexer.Pos
}

// PipelineDecl binds Name to a `|>` chain of Stages. It's a flat slice
// rather than a nested tree of binary Pipe nodes because a pipeline body
// never branches: the chain is always linear, and a slice matches that
// directly instead of adding tree structure nothing will ever use.
//
// Params is nil for a parameterless segment or the runnable pipeline.
// Non-empty, it turns Body into a template the checker specializes at
// each call site rather than a stage list it inlines as-is.
type PipelineDecl struct {
	Name   string
	Params []Param
	Body   []Stage
	Pos    lexer.Pos
}

// ParamKind distinguishes a named segment's two parameter kinds.
type ParamKind int

const (
	// ParamColumn is a bare-identifier parameter, referenced `.name`
	// inside the body and passed a bare column name at the call site.
	ParamColumn ParamKind = iota
	// ParamScalar is a `name: type` parameter, referenced as a bare
	// value inside the body and passed a literal of that type at the
	// call site.
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

// SchemaField is one `name: TypeName ["?"] [@pii] [@deidentify]` pair in
// a schema literal. TypeName is left as the raw identifier text (e.g.
// "string", "int"); resolving it to a value.Kind happens in the checker,
// not the parser. Optional marks a `?` suffix: the field may be absent,
// independent of the PII tag. Deidentify marks @deidentify; the checker
// rejects @pii and @deidentify together, since the parser doesn't
// resolve either tag's meaning.
type SchemaField struct {
	Name       string
	TypeName   string
	Optional   bool
	PII        bool
	Deidentify bool
	Pos        lexer.Pos
}

// SchemaLit is the `{ name: string, age: int }` type literal that
// appears in a source declaration's `schema:` argument. It is a type-level
// construct, distinct from RecordExpr (a value-level record literal used
// inside map). The two are never interchangeable.
type SchemaLit struct {
	Fields []SchemaField
	Pos    lexer.Pos
}
