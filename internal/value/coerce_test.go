package value

import "testing"

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
			// Coerce never knows which format/column it's being called
			// for, so it must leave Stage for the caller to fill in.
			if fail.Stage != "" {
				t.Errorf("Failure.Stage = %q, want empty (caller-assigned)", fail.Stage)
			}
		})
	}
}

// TestCoerceIgnoresPII confirms Coerce's behavior depends only on Kind —
// a @pii string coerces exactly like a plain one; the tag has nothing to
// do with parsing.
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
// cell or (per the caller's convention) a column missing entirely.
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
// coerces exactly like a required one — optionality only changes the
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
// §2 notes): optionality excuses absence, never malformed presence — a
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
