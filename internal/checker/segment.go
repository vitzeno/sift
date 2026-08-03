package checker

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/value"
)

// binding pairs one of a segment's declared parameters with the actual
// argument bound to it at one call site -- exactly what substitution
// needs to rewrite the body (design/segments.md §3), and what dual-site
// diagnostics need to print (§4).
type binding struct {
	param  ast.Param
	colArg string   // valid when param.Kind == ast.ParamColumn
	litArg ast.Expr // valid when param.Kind == ast.ParamScalar
}

// expandSegmentCall resolves a *ast.SegmentCall -- scrub(email),
// adults(18) -- against schema: monomorphize at the call site
// (design/segments.md §3), "C++ template" style, not row polymorphism.
// It copies the target segment's body, substitutes each parameter for
// its bound argument, and checks the copy with the ordinary stage rules
// (expandStages) against the real schema at this point in the pipeline
// -- PII propagation, field existence, and predicate typing all fall out
// of that reuse unchanged, no new rule needed. Any error surfacing from
// inside the substituted body is wrapped with dual-site context before
// it propagates further (§4): which segment, which bindings, and where
// it was instantiated from.
func (c *checker) expandSegmentCall(call *ast.SegmentCall, schema value.Schema, visiting map[string]bool, callerName string) ([]ast.Stage, value.Schema, error) {
	kind, exists := c.namespace[call.Name]
	if !exists {
		return nil, value.Schema{}, errorf(call.Pos, "undefined segment %q", call.Name)
	}
	switch kind {
	case declSource:
		return nil, value.Schema{}, errorf(call.Pos, "source %q cannot be called as a segment", call.Name)
	case declSink:
		return nil, value.Schema{}, errorf(call.Pos, "sink %q cannot be called as a segment", call.Name)
	}

	seg := c.pipelinesByName[call.Name]
	if len(call.Args) != len(seg.Params) {
		return nil, value.Schema{}, errorf(call.Pos,
			"segment %q takes %d argument(s), got %d", call.Name, len(seg.Params), len(call.Args))
	}

	bindings := make(map[string]binding, len(seg.Params))
	for i, param := range seg.Params {
		b, err := bindParam(call.Name, param, call.Args[i])
		if err != nil {
			return nil, value.Schema{}, err
		}
		bindings[param.Name] = b
	}

	if visiting[call.Name] {
		return nil, value.Schema{}, errorf(call.Pos, "pipeline %q is defined in terms of itself", call.Name)
	}
	visiting[call.Name] = true
	defer delete(visiting, call.Name)

	body := substituteStages(seg.Body, bindings)
	stages, newSchema, err := c.expandStages(body, schema, visiting, call.Name)
	if err != nil {
		return nil, value.Schema{}, wrapSegmentError(err, call, seg.Params, bindings, callerName)
	}
	return stages, newSchema, nil
}

// bindParam matches one call argument against the parameter it fills,
// enforcing that a column parameter gets a column-name argument and a
// scalar parameter gets a literal of its declared type (design/segments.md
// §9's PS-G: arity and type mismatches are compile errors with position).
func bindParam(segName string, param ast.Param, arg ast.CallArg) (binding, error) {
	switch param.Kind {
	case ast.ParamColumn:
		if arg.Kind != ast.ArgColumn {
			return binding{}, errorf(arg.Pos,
				"segment %q parameter %q expects a column name, got a literal", segName, param.Name)
		}
		return binding{param: param, colArg: arg.Column}, nil

	case ast.ParamScalar:
		if arg.Kind != ast.ArgScalar {
			return binding{}, errorf(arg.Pos,
				"segment %q parameter %q expects a %s literal, got a column name", segName, param.Name, param.TypeName)
		}
		litKind := scalarLitKind(arg.Literal)
		if wantKind, ok := typeNames[param.TypeName]; !ok || litKind != wantKind {
			return binding{}, errorf(arg.Pos,
				"segment %q parameter %q expects %s, got %s", segName, param.Name, param.TypeName, litKind)
		}
		return binding{param: param, litArg: arg.Literal}, nil

	default:
		panic(fmt.Sprintf("checker: unhandled param kind %v", param.Kind))
	}
}

// scalarLitKind reports a call argument literal's value.Kind. The
// parser's parseCallArg only ever produces one of these four node types
// for a scalar argument, so this is exhaustive by construction.
func scalarLitKind(e ast.Expr) value.Kind {
	switch e.(type) {
	case *ast.IntLit:
		return value.Int
	case *ast.DoubleLit:
		return value.Double
	case *ast.StringLit:
		return value.String
	case *ast.BoolLit:
		return value.Bool
	default:
		panic(fmt.Sprintf("checker: unhandled literal type %T in call argument", e))
	}
}

// wrapSegmentError prepends dual-site context to an error surfacing from
// inside a substituted segment body (design/segments.md §4): the
// segment's name and bindings, and where it was instantiated from. The
// wrapped error keeps the original Pos untouched -- monomorphization
// copies AST nodes without changing their position, so that Pos already
// points at the real location in the segment's own definition; only the
// message gains a line naming the call site on top of it. Wrapping
// happens once per call frame, so an error surfacing through several
// nested parameterized calls accumulates one "in segment / instantiated
// at" pair per frame, innermost first.
func wrapSegmentError(err error, call *ast.SegmentCall, params []ast.Param, bindings map[string]binding, callerName string) error {
	ce, ok := err.(*CheckError)
	if !ok {
		return err
	}
	msg := ce.Msg +
		fmt.Sprintf("\n  in segment %s(%s)", call.Name, formatBindings(params, bindings)) +
		fmt.Sprintf("\n  instantiated at %s:%d", callerName, call.Pos.Line)
	return &CheckError{Pos: ce.Pos, Msg: msg}
}

// formatBindings renders a segment's bindings in declared parameter
// order -- "col = email", "min = 18" -- for the dual-site "in segment
// scrub(col = email)" line.
func formatBindings(params []ast.Param, bindings map[string]binding) string {
	parts := make([]string, len(params))
	for i, param := range params {
		parts[i] = param.Name + " = " + formatBindingValue(bindings[param.Name])
	}
	return strings.Join(parts, ", ")
}

func formatBindingValue(b binding) string {
	if b.param.Kind == ast.ParamColumn {
		return b.colArg
	}
	return formatLiteral(b.litArg)
}

func formatLiteral(e ast.Expr) string {
	switch lit := e.(type) {
	case *ast.IntLit:
		return strconv.FormatInt(lit.Value, 10)
	case *ast.DoubleLit:
		return strconv.FormatFloat(lit.Value, 'g', -1, 64)
	case *ast.StringLit:
		return strconv.Quote(lit.Value)
	case *ast.BoolLit:
		return strconv.FormatBool(lit.Value)
	default:
		panic(fmt.Sprintf("checker: unhandled literal type %T", e))
	}
}

// substituteStages copies stages, replacing every occurrence of a bound
// parameter's name with its call-site argument -- monomorphization's
// "copy the body" step (design/segments.md §3). Positions are preserved
// node-for-node from the segment's own definition; only the text of a
// column/parameter reference changes. That's what makes dual-site
// diagnostics work almost for free: a substituted node's Pos still
// points at where it was actually written, in the segment's own source.
func substituteStages(stages []ast.Stage, bindings map[string]binding) []ast.Stage {
	out := make([]ast.Stage, len(stages))
	for i, s := range stages {
		out[i] = substituteStage(s, bindings)
	}
	return out
}

func substituteStage(s ast.Stage, bindings map[string]binding) ast.Stage {
	switch st := s.(type) {
	case *ast.Filter:
		return &ast.Filter{Pred: substituteExpr(st.Pred, bindings), Pos: st.Pos}

	case *ast.Check:
		return &ast.Check{Cond: substituteExpr(st.Cond, bindings), Reason: st.Reason, Pos: st.Pos}

	case *ast.Map:
		return &ast.Map{Record: substituteRecord(st.Record, bindings), Pos: st.Pos}

	case *ast.Select:
		return &ast.Select{Columns: substituteColumnRefs(st.Columns, bindings), Pos: st.Pos}

	case *ast.Drop:
		return &ast.Drop{Columns: substituteColumnRefs(st.Columns, bindings), Pos: st.Pos}

	case *ast.Rename:
		pairs := make([]ast.RenamePair, len(st.Pairs))
		for i, pr := range st.Pairs {
			pairs[i] = ast.RenamePair{
				Old: substituteColumnName(pr.Old, bindings),
				New: substituteColumnName(pr.New, bindings),
				Pos: pr.Pos,
			}
		}
		return &ast.Rename{Pairs: pairs, Pos: st.Pos}

	case *ast.Limit:
		return st

	case *ast.Offset:
		return st

	case *ast.Declassify:
		return &ast.Declassify{Fn: st.Fn, Columns: substituteColumnRefs(st.Columns, bindings), Pos: st.Pos}

	case *ast.NameRef:
		// Names a source, sink, or parameterless segment -- never a
		// column or scalar, so there's nothing here to substitute.
		return st

	case *ast.SegmentCall:
		args := make([]ast.CallArg, len(st.Args))
		for i, a := range st.Args {
			args[i] = substituteCallArg(a, bindings)
		}
		return &ast.SegmentCall{Name: st.Name, Args: args, Pos: st.Pos}

	default:
		panic(fmt.Sprintf("checker: unhandled stage type %T in substitution", s))
	}
}

func substituteCallArg(a ast.CallArg, bindings map[string]binding) ast.CallArg {
	if a.Kind == ast.ArgColumn {
		return ast.CallArg{Kind: ast.ArgColumn, Column: substituteColumnName(a.Column, bindings), Pos: a.Pos}
	}
	return ast.CallArg{Kind: ast.ArgScalar, Literal: substituteExpr(a.Literal, bindings), Pos: a.Pos}
}

func substituteColumnRefs(cols []ast.ColumnRef, bindings map[string]binding) []ast.ColumnRef {
	out := make([]ast.ColumnRef, len(cols))
	for i, col := range cols {
		out[i] = ast.ColumnRef{Name: substituteColumnName(col.Name, bindings), Pos: col.Pos}
	}
	return out
}

// substituteColumnName replaces name with its bound column argument when
// name is a column parameter in scope; every other identifier -- an
// ordinary column name the segment's author wrote literally, or one that
// happens to collide with a scalar parameter's name -- passes through
// unchanged. The Kind guard is what keeps a scalar parameter's name from
// being mistaken for a column reference here.
func substituteColumnName(name string, bindings map[string]binding) string {
	if b, ok := bindings[name]; ok && b.param.Kind == ast.ParamColumn {
		return b.colArg
	}
	return name
}

func substituteRecord(rec *ast.RecordExpr, bindings map[string]binding) *ast.RecordExpr {
	fields := make([]ast.RecordField, len(rec.Fields))
	for i, f := range rec.Fields {
		fields[i] = ast.RecordField{
			Name:  substituteColumnName(f.Name, bindings),
			Value: substituteExpr(f.Value, bindings),
			Pos:   f.Pos,
		}
	}
	return &ast.RecordExpr{Spread: rec.Spread, Fields: fields, Pos: rec.Pos}
}

func substituteExpr(e ast.Expr, bindings map[string]binding) ast.Expr {
	switch e := e.(type) {
	case *ast.FieldAccess:
		return &ast.FieldAccess{Field: substituteColumnName(e.Field, bindings), Pos: e.Pos}

	case *ast.ParamRef:
		if b, ok := bindings[e.Name]; ok && b.param.Kind == ast.ParamScalar {
			return b.litArg
		}
		// Not one of this segment's scalar parameters -- left as-is;
		// checkExpr reports it as an undefined name.
		return e

	case *ast.IntLit, *ast.DoubleLit, *ast.StringLit, *ast.BoolLit:
		return e

	case *ast.BinaryOp:
		return &ast.BinaryOp{Op: e.Op, Left: substituteExpr(e.Left, bindings), Right: substituteExpr(e.Right, bindings), Pos: e.Pos}

	case *ast.Call:
		args := make([]ast.Expr, len(e.Args))
		for i, a := range e.Args {
			args[i] = substituteExpr(a, bindings)
		}
		return &ast.Call{Fn: e.Fn, Args: args, Pos: e.Pos}

	case *ast.RecordExpr:
		return substituteRecord(e, bindings)

	default:
		panic(fmt.Sprintf("checker: unhandled expression type %T in substitution", e))
	}
}
