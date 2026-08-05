package value

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestCoerceSuccess(t *testing.T) {
	tests := []struct {
		name string
		kind Kind
		raw  string
		want any
	}{
		{"string passthrough", String, "hello", "hello"},
		{"string empty", String, "", ""},
		{"int", Int, "42", 42},
		{"int negative", Int, "-7", -7},
		{"double", Double, "3.14", 3.14},
		{"bool true", Bool, "true", true},
		{"bool false", Bool, "false", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, fail := Coerce(Type{Kind: tt.kind}, tt.raw)
			if fail != nil {
				t.Fatalf("Coerce(%q) failed: %+v", tt.raw, fail)
			}
			if got != tt.want {
				t.Errorf("Coerce(%q) = %#v, want %#v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCoerceFailure(t *testing.T) {
	tests := []struct {
		name    string
		kind    Kind
		raw     string
		wantSub string
	}{
		{"bad int", Int, "abc", `cannot parse "abc" as int`},
		{"bad double", Double, "abc", `cannot parse "abc" as double`},
		{"bad bool", Bool, "maybe", `cannot parse "maybe" as bool`},
		{"empty int", Int, "", `cannot parse "" as int`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, fail := Coerce(Type{Kind: tt.kind}, tt.raw)
			if fail == nil {
				t.Fatalf("Coerce(%q) = %#v, nil; want a Failure", tt.raw, got)
			}
			if fail.Reason != tt.wantSub {
				t.Errorf("Failure.Reason = %q, want %q", fail.Reason, tt.wantSub)
			}
			// Coerce never knows which format or column it's called for,
			// so it leaves Stage for the caller to fill in.
			if fail.Stage != "" {
				t.Errorf("Failure.Stage = %q, want empty (caller-assigned)", fail.Stage)
			}
		})
	}
}

// TestCoerceIgnoresPII confirms Coerce's behavior depends only on Kind: a
// @pii string coerces exactly like a plain one, since the tag has
// nothing to do with parsing.
func TestCoerceIgnoresPII(t *testing.T) {
	got, fail := Coerce(Type{Kind: String, PII: true}, "secret")
	if fail != nil {
		t.Fatalf("Coerce failed: %+v", fail)
	}
	if got != "secret" {
		t.Errorf("Coerce = %#v, want \"secret\"", got)
	}
}

// TestCoerceOptionalAbsent is OF-A/OF-C's unit-level half
// (design/optional-fields.md §2): a blank or whitespace-only cell against
// an Optional Type yields Absent, whether the blank came from an empty
// cell or, by the caller's convention, a column missing entirely.
func TestCoerceOptionalAbsent(t *testing.T) {
	tests := []struct {
		name string
		kind Kind
		raw  string
	}{
		{"empty string", String, ""},
		{"empty int", Int, ""},
		{"whitespace only", String, "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, fail := Coerce(Type{Kind: tt.kind, Optional: true}, tt.raw)
			if fail != nil {
				t.Fatalf("Coerce(%q) failed: %+v", tt.raw, fail)
			}
			if _, ok := got.(Absent); !ok {
				t.Errorf("Coerce(%q) = %#v, want Absent", tt.raw, got)
			}
		})
	}
}

// TestCoerceOptionalPresent confirms an optional field with a real value
// coerces exactly like a required one: optionality only changes the
// blank-cell case, not parsing.
func TestCoerceOptionalPresent(t *testing.T) {
	got, fail := Coerce(Type{Kind: Int, Optional: true}, "42")
	if fail != nil {
		t.Fatalf("Coerce failed: %+v", fail)
	}
	if got != 42 {
		t.Errorf("Coerce = %#v, want 42", got)
	}
}

// TestCoerceOptionalUnparseableIsFailure is OF-D (design/optional-fields.md
// §2 notes): optionality excuses absence, never malformed presence. A
// present-but-garbage cell in an optional field is still a row Failure.
func TestCoerceOptionalUnparseableIsFailure(t *testing.T) {
	got, fail := Coerce(Type{Kind: Int, Optional: true}, "not-a-number")
	if fail == nil {
		t.Fatalf("Coerce = %#v, nil; want a Failure for garbage in an optional field", got)
	}
	if fail.Reason != `cannot parse "not-a-number" as int` {
		t.Errorf("Failure.Reason = %q, want %q", fail.Reason, `cannot parse "not-a-number" as int`)
	}
}

// TestCoerceDateDefaultFormat is DATE-A: with no dateFormat argument at
// all, Coerce falls back to the ISO-8601 default.
func TestCoerceDateDefaultFormat(t *testing.T) {
	got, fail := Coerce(Type{Kind: Date}, "2026-01-05")
	if fail != nil {
		t.Fatalf("Coerce failed: %+v", fail)
	}
	want := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	if !time.Time(got.(DateValue)).Equal(want) {
		t.Errorf("Coerce = %v, want %v", time.Time(got.(DateValue)), want)
	}
}

// TestCoerceDateExplicitFormat is DATE-B: a dateFormat argument, when
// given, drives parsing instead of the default, proving day/month order
// is actually read from it rather than assumed.
func TestCoerceDateExplicitFormat(t *testing.T) {
	got, fail := Coerce(Type{Kind: Date}, "05/01/2026", "02/01/2006")
	if fail != nil {
		t.Fatalf("Coerce failed: %+v", fail)
	}
	want := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	if !time.Time(got.(DateValue)).Equal(want) {
		t.Errorf("Coerce = %v, want %v", time.Time(got.(DateValue)), want)
	}
}

// TestCoerceDateFailure is DATE-C: a cell that doesn't match the format,
// and separately one that matches the format but names a calendar date
// that doesn't exist, are both row failures, not panics.
func TestCoerceDateFailure(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"format mismatch", "not-a-date"},
		{"impossible calendar date", "2026-02-30"},
		{"impossible month", "2026-13-01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, fail := Coerce(Type{Kind: Date}, tt.raw)
			if fail == nil {
				t.Fatalf("Coerce(%q) = %#v, nil; want a Failure", tt.raw, got)
			}
			want := `cannot parse "` + tt.raw + `" as date`
			if fail.Reason != want {
				t.Errorf("Failure.Reason = %q, want %q", fail.Reason, want)
			}
		})
	}
}

// TestCoerceDateOptionalAbsent confirms the Optional blank-cell carve-out
// (design/optional-fields.md §2) applies to Date exactly like every other
// Kind, with no Date-specific code needed for it.
func TestCoerceDateOptionalAbsent(t *testing.T) {
	got, fail := Coerce(Type{Kind: Date, Optional: true}, "")
	if fail != nil {
		t.Fatalf("Coerce failed: %+v", fail)
	}
	if _, ok := got.(Absent); !ok {
		t.Errorf("Coerce(\"\") = %#v, want Absent", got)
	}
}

// TestCoerceDecimalParsesExactly is DEC-A's parse half: "19.99" parses
// to exactly 19.99, not a float64-tainted approximation. Uses .Equal(),
// not Go's bare ==, since decimal.Decimal (which DecimalValue wraps) is
// a struct holding a *big.Int (design/decimal.md §2) -- the same reason
// this file's Date tests never lean on bare == either.
func TestCoerceDecimalParsesExactly(t *testing.T) {
	got, fail := Coerce(Type{Kind: Decimal}, "19.99")
	if fail != nil {
		t.Fatalf("Coerce failed: %+v", fail)
	}
	want := decimal.NewFromFloat(19.99)
	d, ok := got.(DecimalValue)
	if !ok {
		t.Fatalf("Coerce = %#v (%T), want DecimalValue", got, got)
	}
	if !decimal.Decimal(d).Equal(want) {
		t.Errorf("Coerce(\"19.99\") = %s, want %s", d, want)
	}
	if d.String() != "19.99" {
		t.Errorf("Coerce(\"19.99\").String() = %q, want %q", d.String(), "19.99")
	}
}

// TestCoerceDecimalPreservesTrailingZeros is DEC-A's other half, and the
// one a naive implementation gets wrong: decimal.Decimal's own String()
// silently strips trailing zeros ("5.00" -> "5", confirmed empirically),
// even though its internal exponent still remembers the original scale.
// DecimalValue's own String()/MarshalJSON must render at that remembered
// scale instead, so "what you parse is what comes back out" (§1) is
// actually true, not just documented as an intent.
func TestCoerceDecimalPreservesTrailingZeros(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"5.00", "5.00"},
		{"0.00", "0.00"},
		{"100.00", "100.00"},
		{"100", "100"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, fail := Coerce(Type{Kind: Decimal}, tt.raw)
			if fail != nil {
				t.Fatalf("Coerce failed: %+v", fail)
			}
			d := got.(DecimalValue)
			if d.String() != tt.want {
				t.Errorf("Coerce(%q).String() = %q, want %q", tt.raw, d.String(), tt.want)
			}
		})
	}
}

// TestCoerceDecimalFailure is DEC-B: a non-numeric cell is a row
// failure, same shape as a bad int/double cell, not a panic.
func TestCoerceDecimalFailure(t *testing.T) {
	got, fail := Coerce(Type{Kind: Decimal}, "not-a-number")
	if fail == nil {
		t.Fatalf("Coerce(%q) = %#v, nil; want a Failure", "not-a-number", got)
	}
	want := `cannot parse "not-a-number" as decimal`
	if fail.Reason != want {
		t.Errorf("Failure.Reason = %q, want %q", fail.Reason, want)
	}
}

// TestCoerceDecimalOptionalAbsent confirms the Optional blank-cell
// carve-out applies to Decimal exactly like every other Kind, with no
// Decimal-specific code needed for it.
func TestCoerceDecimalOptionalAbsent(t *testing.T) {
	got, fail := Coerce(Type{Kind: Decimal, Optional: true}, "")
	if fail != nil {
		t.Fatalf("Coerce failed: %+v", fail)
	}
	if _, ok := got.(Absent); !ok {
		t.Errorf("Coerce(\"\") = %#v, want Absent", got)
	}
}
