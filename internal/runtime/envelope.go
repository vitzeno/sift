package runtime

import (
	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/value"
)

// envelopeSchema is the fixed schema every routed failure is written
// against (design-errors.md §4): provenance and reason only, never a
// pipeline's real fields. Every field here is a plain, clean scalar, so
// an error sink satisfies the checker's unmasked-@pii sink rule with no
// special-casing: there's nothing pipeline-derived left to leak.
var envelopeSchema = value.Schema{Fields: []value.Field{
	{Name: "source", Type: value.Type{Kind: value.String}},
	{Name: "ordinal", Type: value.Type{Kind: value.Int}},
	{Name: "offset", Type: value.Type{Kind: value.Int}},
	{Name: "reason", Type: value.Type{Kind: value.String}},
	{Name: "stage", Type: value.Type{Kind: value.String}},
}}

// envelopeRow builds the Row a routed failure is written as: provenance
// (design-errors.md's "source" field is Provenance.Source, the declared
// source name, matching what Provenance has always meant here, not the
// source's file path) plus the failure's reason and originating stage.
func envelopeRow(prov value.Provenance, fail *value.Failure) value.Row {
	return value.Row{
		Fields: map[string]any{
			"source":  prov.Source,
			"ordinal": prov.Ordinal,
			"offset":  prov.Offset,
			"reason":  fail.Reason,
			"stage":   fail.Stage,
		},
		Prov: prov,
	}
}

// NewErrorSink builds the sink for a program's `on error |> <name>`
// target. It always builds against envelopeSchema, never a pipeline's
// schema (design-errors.md §4); the checker never computes or threads
// one in for it (internal/checker/errorpolicy.go).
func NewErrorSink(target *ast.SinkDecl) (Sink, error) {
	return NewSink(target.Format, SinkOptions{Name: target.Name, Path: target.Path, Schema: envelopeSchema})
}
