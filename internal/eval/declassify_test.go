package eval

import (
	"testing"

	"github.com/vitzeno/sift/internal/ast"
)

// TestDeclassifyMatchesExpressionPositionCall confirms the exported
// Declassify function backs both the stage form and the expression form
// identically: calling it directly must produce the same result as
// evaluating the equivalent expression-position Call node, since
// runtime's Declassify stage calls this function directly rather than
// going through Eval/ast.Call at all.
func TestDeclassifyMatchesExpressionPositionCall(t *testing.T) {
	for _, fn := range []string{"mask", "hash", "redact"} {
		direct := Declassify(fn, "secret")
		viaCall := Eval(&ast.Call{Fn: fn, Args: []ast.Expr{&ast.StringLit{Value: "secret"}}}, row(nil))
		if direct != viaCall {
			t.Errorf("%s: Declassify = %q, evalCall = %q, want identical", fn, direct, viaCall)
		}
	}
}

func TestDeclassifyUnknownFnPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Declassify did not panic on an unknown declassifier name")
		}
	}()
	Declassify("bogus", "secret")
}
