package eval

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

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

// TestEvalDateComparison exercises all six comparison operators on
// value.DateValue via FieldAccess (design/date.md §3): there's no
// ast.DateLit, so a Date value can only reach Eval through a row field,
// never a literal node.
func TestEvalDateComparison(t *testing.T) {
	earlier := value.DateValue(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))
	later := value.DateValue(time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC))
	fields := map[string]any{"a": earlier, "b": later}

	tests := []struct {
		name string
		op   lexer.Kind
		want bool
	}{
		{"a < b", lexer.LT, true},
		{"a > b", lexer.GT, false},
		{"a <= b", lexer.LE, true},
		{"a >= b", lexer.GE, false},
		{"a == b", lexer.EQ, false},
		{"a != b", lexer.NE, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := &ast.BinaryOp{Op: tt.op, Left: &ast.FieldAccess{Field: "a"}, Right: &ast.FieldAccess{Field: "b"}}
			got := Eval(expr, row(fields))
			if got != tt.want {
				t.Errorf("Eval(a %s b) = %#v, want %#v", tt.name, got, tt.want)
			}
		})
	}
}

// TestEvalDateEqualityUsesTimeEqualNotBareEquals is DATE-E: two
// value.DateValues that denote the exact same instant but differ in
// internal representation (time.UTC vs an equivalent FixedZone -- bare
// Go == returns false for this pair, confirmed empirically, while
// time.Time.Equal returns true) must still compare == true through
// Sift's == operator. This is the regression a bare `left == right`
// fallthrough in evalBinaryOp would silently reintroduce.
func TestEvalDateEqualityUsesTimeEqualNotBareEquals(t *testing.T) {
	a := value.DateValue(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))
	b := value.DateValue(time.Date(2026, 1, 5, 0, 0, 0, 0, time.FixedZone("UTC", 0)))
	//nolint:staticcheck // QF1009: the bare == here is the point of this
	// check, not a mistake -- it confirms the fixture actually diverges
	// from .Equal() before asserting Sift's own == uses .Equal() below.
	if time.Time(a) == time.Time(b) {
		t.Fatal("test setup invalid: a and b must be bare-== unequal despite denoting the same instant")
	}
	fields := map[string]any{"a": a, "b": b}

	eq := &ast.BinaryOp{Op: lexer.EQ, Left: &ast.FieldAccess{Field: "a"}, Right: &ast.FieldAccess{Field: "b"}}
	if got := Eval(eq, row(fields)); got != true {
		t.Errorf("Eval(a == b) = %#v, want true (same instant, different representation)", got)
	}

	ne := &ast.BinaryOp{Op: lexer.NE, Left: &ast.FieldAccess{Field: "a"}, Right: &ast.FieldAccess{Field: "b"}}
	if got := Eval(ne, row(fields)); got != false {
		t.Errorf("Eval(a != b) = %#v, want false (same instant, different representation)", got)
	}
}

// TestEvalDecimalArithmetic is DEC-C: decimal-decimal arithmetic is
// exact, and a bare int/double literal on either side promotes to
// decimal at eval time exactly like the checker allowed it to at
// compile time (design/decimal.md §2).
func TestEvalDecimalArithmetic(t *testing.T) {
	price := value.DecimalValue(decimal.RequireFromString("19.99"))
	discount := value.DecimalValue(decimal.RequireFromString("5.00"))
	fields := map[string]any{"price": price, "discount": discount}

	tests := []struct {
		name string
		expr *ast.BinaryOp
		want string
	}{
		{
			"decimal - decimal",
			&ast.BinaryOp{Op: lexer.MINUS, Left: &ast.FieldAccess{Field: "price"}, Right: &ast.FieldAccess{Field: "discount"}},
			"14.99",
		},
		{
			"decimal * int literal",
			&ast.BinaryOp{Op: lexer.STAR, Left: &ast.FieldAccess{Field: "price"}, Right: &ast.IntLit{Value: 3}},
			"59.97",
		},
		{
			"decimal * double literal",
			&ast.BinaryOp{Op: lexer.STAR, Left: &ast.FieldAccess{Field: "price"}, Right: &ast.DoubleLit{Value: 0.08}},
			"1.5992",
		},
		{
			"double literal * decimal (literal on the left)",
			&ast.BinaryOp{Op: lexer.STAR, Left: &ast.DoubleLit{Value: 0.08}, Right: &ast.FieldAccess{Field: "price"}},
			"1.5992",
		},
		{
			"int literal + decimal (literal on the left)",
			&ast.BinaryOp{Op: lexer.PLUS, Left: &ast.IntLit{Value: 1}, Right: &ast.FieldAccess{Field: "discount"}},
			"6",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Eval(tt.expr, row(fields)).(value.DecimalValue)
			if !ok {
				t.Fatalf("Eval(%s) = %#v, want value.DecimalValue", tt.name, Eval(tt.expr, row(fields)))
			}
			want := decimal.RequireFromString(tt.want)
			if !decimal.Decimal(got).Equal(want) {
				t.Errorf("Eval(%s) = %s, want %s", tt.name, got, want)
			}
		})
	}
}

// TestEvalDecimalComparison exercises all six comparison operators on
// value.DecimalValue via FieldAccess, mirroring TestEvalDateComparison.
func TestEvalDecimalComparison(t *testing.T) {
	smaller := value.DecimalValue(decimal.RequireFromString("5.00"))
	larger := value.DecimalValue(decimal.RequireFromString("19.99"))
	fields := map[string]any{"a": smaller, "b": larger}

	tests := []struct {
		name string
		op   lexer.Kind
		want bool
	}{
		{"a < b", lexer.LT, true},
		{"a > b", lexer.GT, false},
		{"a <= b", lexer.LE, true},
		{"a >= b", lexer.GE, false},
		{"a == b", lexer.EQ, false},
		{"a != b", lexer.NE, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := &ast.BinaryOp{Op: tt.op, Left: &ast.FieldAccess{Field: "a"}, Right: &ast.FieldAccess{Field: "b"}}
			got := Eval(expr, row(fields))
			if got != tt.want {
				t.Errorf("Eval(a %s b) = %#v, want %#v", tt.name, got, tt.want)
			}
		})
	}
}

// TestEvalDecimalEqualityUsesDecimalEqualNotBareEquals is DEC-E: two
// value.DecimalValues that denote the same number but differ in
// internal representation ("1.50" vs "1.5" -- bare Go == returns false
// for this pair, confirmed empirically, while decimal.Decimal.Equal
// returns true) must still compare == true through Sift's == operator.
// This is the regression a bare `left == right` fallthrough in
// evalBinaryOp would silently reintroduce.
func TestEvalDecimalEqualityUsesDecimalEqualNotBareEquals(t *testing.T) {
	a := value.DecimalValue(decimal.RequireFromString("1.50"))
	b := value.DecimalValue(decimal.RequireFromString("1.5"))
	if a == b {
		t.Fatal("test setup invalid: a and b must be bare-== unequal despite denoting the same number")
	}
	fields := map[string]any{"a": a, "b": b}

	eq := &ast.BinaryOp{Op: lexer.EQ, Left: &ast.FieldAccess{Field: "a"}, Right: &ast.FieldAccess{Field: "b"}}
	if got := Eval(eq, row(fields)); got != true {
		t.Errorf("Eval(a == b) = %#v, want true (same number, different representation)", got)
	}

	ne := &ast.BinaryOp{Op: lexer.NE, Left: &ast.FieldAccess{Field: "a"}, Right: &ast.FieldAccess{Field: "b"}}
	if got := Eval(ne, row(fields)); got != false {
		t.Errorf("Eval(a != b) = %#v, want false (same number, different representation)", got)
	}
}

// TestEvalDateTimeComparison is DT-D: two value.DateTimeValues sharing
// the same calendar date but different times of day order correctly by
// time-of-day, proving DateTime is genuinely finer-grained than Date, not
// just Date with extra digits ignored.
func TestEvalDateTimeComparison(t *testing.T) {
	earlier := value.DateTimeValue(time.Date(2026, 7, 31, 4, 10, 25, 0, time.UTC))
	later := value.DateTimeValue(time.Date(2026, 7, 31, 22, 0, 0, 0, time.UTC))
	fields := map[string]any{"a": earlier, "b": later}

	tests := []struct {
		name string
		op   lexer.Kind
		want bool
	}{
		{"a < b", lexer.LT, true},
		{"a > b", lexer.GT, false},
		{"a <= b", lexer.LE, true},
		{"a >= b", lexer.GE, false},
		{"a == b", lexer.EQ, false},
		{"a != b", lexer.NE, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := &ast.BinaryOp{Op: tt.op, Left: &ast.FieldAccess{Field: "a"}, Right: &ast.FieldAccess{Field: "b"}}
			got := Eval(expr, row(fields))
			if got != tt.want {
				t.Errorf("Eval(a %s b) = %#v, want %#v", tt.name, got, tt.want)
			}
		})
	}
}

// TestEvalDateTimeEqualityUsesTimeEqualNotBareEquals is DT-E: two
// value.DateTimeValues that denote the exact same instant but differ in
// internal representation (time.UTC vs an equivalent FixedZone -- bare Go
// == returns false for this pair, confirmed empirically, while
// time.Time.Equal returns true) must still compare == true through
// Sift's == operator, now exercised through the shared OrderedValue path
// rather than a hand-written branch.
func TestEvalDateTimeEqualityUsesTimeEqualNotBareEquals(t *testing.T) {
	a := value.DateTimeValue(time.Date(2026, 7, 31, 4, 10, 25, 0, time.UTC))
	b := value.DateTimeValue(time.Date(2026, 7, 31, 4, 10, 25, 0, time.FixedZone("UTC", 0)))
	//nolint:staticcheck // QF1009: the bare == here is the point of this
	// check, not a mistake -- it confirms the fixture actually diverges
	// from .Equal() before asserting Sift's own == uses .Equal() below.
	if time.Time(a) == time.Time(b) {
		t.Fatal("test setup invalid: a and b must be bare-== unequal despite denoting the same instant")
	}
	fields := map[string]any{"a": a, "b": b}

	eq := &ast.BinaryOp{Op: lexer.EQ, Left: &ast.FieldAccess{Field: "a"}, Right: &ast.FieldAccess{Field: "b"}}
	if got := Eval(eq, row(fields)); got != true {
		t.Errorf("Eval(a == b) = %#v, want true (same instant, different representation)", got)
	}

	ne := &ast.BinaryOp{Op: lexer.NE, Left: &ast.FieldAccess{Field: "a"}, Right: &ast.FieldAccess{Field: "b"}}
	if got := Eval(ne, row(fields)); got != false {
		t.Errorf("Eval(a != b) = %#v, want false (same instant, different representation)", got)
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
