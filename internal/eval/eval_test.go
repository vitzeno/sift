package eval

import (
	"testing"

	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/lexer"
	"github.com/vitzeno/sift/internal/value"
)

func row(fields map[string]any) value.Row {
	return value.Row{Fields: fields}
}

func TestEvalLiterals(t *testing.T) {
	tests := []struct {
		name string
		expr ast.Expr
		want any
	}{
		{"int", &ast.IntLit{Value: 42}, 42},
		{"double", &ast.DoubleLit{Value: 3.14}, 3.14},
		{"string", &ast.StringLit{Value: "hi"}, "hi"},
		{"bool", &ast.BoolLit{Value: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Eval(tt.expr, row(nil))
			if got != tt.want {
				t.Errorf("Eval(%s) = %#v (%T), want %#v (%T)", tt.name, got, got, tt.want, tt.want)
			}
		})
	}
}

func TestEvalFieldAccess(t *testing.T) {
	r := row(map[string]any{"name": "Ada", "age": 42})
	if got := Eval(&ast.FieldAccess{Field: "name"}, r); got != "Ada" {
		t.Errorf("Eval(.name) = %#v, want \"Ada\"", got)
	}
	if got := Eval(&ast.FieldAccess{Field: "age"}, r); got != 42 {
		t.Errorf("Eval(.age) = %#v, want 42", got)
	}
}

// TestEvalCoalesceResolvesAbsent is OF-E's runtime half: ?? evaluates
// the right side only when the left side is Absent.
func TestEvalCoalesceResolvesAbsent(t *testing.T) {
	r := row(map[string]any{"phone": value.Absent{}})
	expr := &ast.BinaryOp{Op: lexer.COALESCE, Left: &ast.FieldAccess{Field: "phone"}, Right: &ast.StringLit{Value: "n/a"}}
	if got := Eval(expr, r); got != "n/a" {
		t.Errorf("Eval(.phone ?? \"n/a\") = %#v, want \"n/a\" for an absent phone", got)
	}
}

// TestEvalCoalescePassesThroughPresent is ??'s other half: a present
// left value is returned as-is, and the right side is never evaluated.
// Proven by making the right side a call to an unknown function, which
// evalCall panics on if it's ever reached.
func TestEvalCoalescePassesThroughPresent(t *testing.T) {
	r := row(map[string]any{"phone": "555-1234"})
	poison := &ast.Call{Fn: "not-a-real-function", Args: []ast.Expr{&ast.StringLit{Value: "n/a"}}}
	expr := &ast.BinaryOp{Op: lexer.COALESCE, Left: &ast.FieldAccess{Field: "phone"}, Right: poison}
	if got := Eval(expr, r); got != "555-1234" {
		t.Errorf("Eval(.phone ?? panic) = %#v, want \"555-1234\"", got)
	}
}

// TestEvalBinaryOpPropagatesAbsent is OF-F's runtime half: every operator
// other than ?? returns Absent when either operand is Absent, instead of
// panicking on the type assertion its normal case would otherwise do.
func TestEvalBinaryOpPropagatesAbsent(t *testing.T) {
	absentRow := row(map[string]any{"age": value.Absent{}})
	tests := []struct {
		name string
		expr ast.Expr
	}{
		{"+", &ast.BinaryOp{Op: lexer.PLUS, Left: &ast.FieldAccess{Field: "age"}, Right: &ast.IntLit{Value: 1}}},
		{"==", &ast.BinaryOp{Op: lexer.EQ, Left: &ast.FieldAccess{Field: "age"}, Right: &ast.IntLit{Value: 1}}},
		{">=", &ast.BinaryOp{Op: lexer.GE, Left: &ast.FieldAccess{Field: "age"}, Right: &ast.IntLit{Value: 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Eval(tt.expr, absentRow)
			if _, absent := got.(value.Absent); !absent {
				t.Errorf("Eval = %#v, want value.Absent", got)
			}
		})
	}
}

// TestEvalCallPropagatesAbsent is OF-F's runtime half for function calls:
// upper(.phone) on an absent phone yields Absent, not a type-assertion
// panic on the missing string.
func TestEvalCallPropagatesAbsent(t *testing.T) {
	r := row(map[string]any{"phone": value.Absent{}})
	call := &ast.Call{Fn: "upper", Args: []ast.Expr{&ast.FieldAccess{Field: "phone"}}}
	got := Eval(call, r)
	if _, absent := got.(value.Absent); !absent {
		t.Errorf("Eval(upper(.phone)) = %#v, want value.Absent", got)
	}
}

func TestEvalBinaryOpArithmeticAndComparison(t *testing.T) {
	tests := []struct {
		name string
		op   lexer.Kind
		l, r any
		want any
	}{
		{"int +", lexer.PLUS, 2, 3, 5},
		{"int -", lexer.MINUS, 5, 3, 2},
		{"int *", lexer.STAR, 4, 3, 12},
		{"int /", lexer.SLASH, 10, 3, 3},
		{"double +", lexer.PLUS, 1.5, 2.5, 4.0},
		{"string +", lexer.PLUS, "foo", "bar", "foobar"},
		{"int <", lexer.LT, 1, 2, true},
		{"int >=", lexer.GE, 18, 18, true},
		{"double <=", lexer.LE, 2.5, 2.5, true},
		{"eq", lexer.EQ, "a", "a", true},
		{"ne", lexer.NE, "a", "b", true},
		{"and", lexer.AND, true, false, false},
		{"or", lexer.OR, true, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := &ast.BinaryOp{Op: tt.op, Left: litOf(tt.l), Right: litOf(tt.r)}
			got := Eval(expr, row(nil))
			if got != tt.want {
				t.Errorf("Eval(%v %s %v) = %#v, want %#v", tt.l, tt.name, tt.r, got, tt.want)
			}
		})
	}
}

// litOf wraps a Go value as the matching ast literal node, so the table
// above can stay declarative instead of hand-building each case.
func litOf(v any) ast.Expr {
	switch v := v.(type) {
	case int:
		return &ast.IntLit{Value: int64(v)}
	case float64:
		return &ast.DoubleLit{Value: v}
	case string:
		return &ast.StringLit{Value: v}
	case bool:
		return &ast.BoolLit{Value: v}
	default:
		panic("litOf: unsupported type")
	}
}

func TestEvalCallDeclassifiers(t *testing.T) {
	arg := &ast.StringLit{Value: "secret"}

	if got := Eval(&ast.Call{Fn: "mask", Args: []ast.Expr{arg}}, row(nil)); got != "******" {
		t.Errorf("mask(\"secret\") = %q, want 6 asterisks", got)
	}
	if got := Eval(&ast.Call{Fn: "redact", Args: []ast.Expr{arg}}, row(nil)); got != "[REDACTED]" {
		t.Errorf("redact(\"secret\") = %q, want \"[REDACTED]\"", got)
	}
	hash1 := Eval(&ast.Call{Fn: "hash", Args: []ast.Expr{arg}}, row(nil)).(string)
	hash2 := Eval(&ast.Call{Fn: "hash", Args: []ast.Expr{&ast.StringLit{Value: "secret"}}}, row(nil)).(string)
	if hash1 != hash2 {
		t.Error("hash(\"secret\") is not deterministic across calls")
	}
	if hash1 == "secret" || len(hash1) != 64 {
		t.Errorf("hash(\"secret\") = %q, want a 64-char hex SHA-256 digest", hash1)
	}
}

func TestEvalCallStringFunctions(t *testing.T) {
	tests := []struct {
		fn   string
		arg  string
		want string
	}{
		{"upper", "shout", "SHOUT"},
		{"lower", "WHISPER", "whisper"},
		{"trim", "  padded  ", "padded"},
	}
	for _, tt := range tests {
		call := &ast.Call{Fn: tt.fn, Args: []ast.Expr{&ast.StringLit{Value: tt.arg}}}
		if got := Eval(call, row(nil)); got != tt.want {
			t.Errorf("%s(%q) = %q, want %q", tt.fn, tt.arg, got, tt.want)
		}
	}
}

func TestEvalNestedCall(t *testing.T) {
	// lower(trim(.email))
	call := &ast.Call{Fn: "lower", Args: []ast.Expr{
		&ast.Call{Fn: "trim", Args: []ast.Expr{&ast.FieldAccess{Field: "email"}}},
	}}
	r := row(map[string]any{"email": "  ADA@EXAMPLE.COM  "})
	if got := Eval(call, r); got != "ada@example.com" {
		t.Errorf("lower(trim(.email)) = %q, want %q", got, "ada@example.com")
	}
}

func TestEvalRecordSpreadAndOverride(t *testing.T) {
	rec := &ast.RecordExpr{
		Spread: "row",
		Fields: []ast.RecordField{
			{Name: "age", Value: &ast.BinaryOp{
				Op:    lexer.GE,
				Left:  &ast.FieldAccess{Field: "age"},
				Right: &ast.IntLit{Value: 18},
			}},
			{Name: "note", Value: &ast.StringLit{Value: "checked"}},
		},
	}
	r := row(map[string]any{"name": "Ada", "age": 42})
	got := EvalRecord(rec, r)

	want := map[string]any{"name": "Ada", "age": true, "note": "checked"}
	if len(got) != len(want) {
		t.Fatalf("EvalRecord = %#v, want %#v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("EvalRecord[%q] = %#v, want %#v", k, got[k], v)
		}
	}
}

func TestEvalRecordNoSpread(t *testing.T) {
	rec := &ast.RecordExpr{Fields: []ast.RecordField{
		{Name: "greeting", Value: &ast.StringLit{Value: "hi"}},
	}}
	r := row(map[string]any{"name": "Ada", "age": 42})
	got := EvalRecord(rec, r)

	if len(got) != 1 || got["greeting"] != "hi" {
		t.Errorf("EvalRecord = %#v, want only {greeting: hi}", got)
	}
}
