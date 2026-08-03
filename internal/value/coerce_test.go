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
