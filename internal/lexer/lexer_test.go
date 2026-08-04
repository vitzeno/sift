package lexer

import "testing"

// tok is a shorthand for the fields tests care about: Kind and Lit.
// Pos is checked separately, only where it matters, to keep the token
// tables below readable.
type tok struct {
	kind Kind
	lit  string
}

func collect(t *testing.T, src string) []tok {
	t.Helper()
	l := New(src)
	var got []tok
	for {
		tk := l.Next()
		got = append(got, tok{tk.Kind, tk.Lit})
		if tk.Kind == EOF {
			return got
		}
		if tk.Kind == ILLEGAL {
			// Let the caller see the ILLEGAL token too, but stop before
			// an infinite loop if scanning can't make progress.
			return got
		}
	}
}

func assertTokens(t *testing.T, src string, want []tok) {
	t.Helper()
	got := collect(t, src)
	if len(got) != len(want) {
		t.Fatalf("token count = %d, want %d\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d = %+v, want %+v\n got: %v\nwant: %v", i, got[i], want[i], got, want)
		}
	}
}

// TestAdultsFilterTokens is design.md §7 Case A's adults.sift, tokenized
// end to end. This is the token table CLAUDE.md's Definition of Done
// asks for.
func TestAdultsFilterTokens(t *testing.T) {
	src := `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("adults.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> out
}`

	want := []tok{
		{SOURCE, ""}, {IDENT, "in"}, {ASSIGN, ""}, {IDENT, "csv"}, {LPAREN, ""},
		{STRING, "people.csv"}, {COMMA, ""}, {IDENT, "schema"}, {COLON, ""},
		{LBRACE, ""}, {IDENT, "name"}, {COLON, ""}, {IDENT, "string"}, {COMMA, ""},
		{IDENT, "age"}, {COLON, ""}, {IDENT, "int"}, {RBRACE, ""}, {RPAREN, ""},

		{SINK, ""}, {IDENT, "out"}, {ASSIGN, ""}, {IDENT, "jsonl"}, {LPAREN, ""},
		{STRING, "adults.jsonl"}, {RPAREN, ""},

		{PIPELINE, ""}, {IDENT, "main"}, {LBRACE, ""},
		{IDENT, "in"}, {PIPE, ""}, {IDENT, "filter"}, {LPAREN, ""},
		{DOT, ""}, {IDENT, "age"}, {GE, ""}, {INT, "18"}, {RPAREN, ""},
		{PIPE, ""}, {IDENT, "out"},
		{RBRACE, ""},

		{EOF, ""},
	}
	assertTokens(t, src, want)
}

// TestPIIAndDeclassify covers design.md §7 Case B's shape: a @pii tag on
// a schema field, and the mask() call that clears it.
func TestPIIAndDeclassify(t *testing.T) {
	src := `email: string @pii
map({ email: mask(.email) })`

	want := []tok{
		{IDENT, "email"}, {COLON, ""}, {IDENT, "string"}, {AT, ""}, {IDENT, "pii"},
		{IDENT, "map"}, {LPAREN, ""}, {LBRACE, ""}, {IDENT, "email"}, {COLON, ""},
		{IDENT, "mask"}, {LPAREN, ""}, {DOT, ""}, {IDENT, "email"}, {RPAREN, ""},
		{RBRACE, ""}, {RPAREN, ""},
		{EOF, ""},
	}
	assertTokens(t, src, want)
}

// TestOptionalSchemaField covers design/optional-fields.md's `T?` schema
// syntax, alone and combined with @pii (§4: the two tags coexist).
func TestOptionalSchemaField(t *testing.T) {
	src := `phone: string?
email: string? @pii`

	want := []tok{
		{IDENT, "phone"}, {COLON, ""}, {IDENT, "string"}, {QUESTION, ""},
		{IDENT, "email"}, {COLON, ""}, {IDENT, "string"}, {QUESTION, ""}, {AT, ""}, {IDENT, "pii"},
		{EOF, ""},
	}
	assertTokens(t, src, want)
}

// TestCoalesceOperator covers design/optional-fields.md's `??` discharge
// operator: two "?" lex as one COALESCE token, not two QUESTIONs, and a
// lone "?" (schema syntax) still lexes as QUESTION.
func TestCoalesceOperator(t *testing.T) {
	assertTokens(t, `.phone ?? "n/a"`, []tok{
		{DOT, ""}, {IDENT, "phone"}, {COALESCE, ""}, {STRING, "n/a"},
		{EOF, ""},
	})
	assertTokens(t, "phone: string?", []tok{
		{IDENT, "phone"}, {COLON, ""}, {IDENT, "string"}, {QUESTION, ""},
		{EOF, ""},
	})
}

// TestRouteArrow covers design/routing.md §7's one new token: "=>" lexes
// as one ARROW, not ASSIGN followed by GT, and a lone "=" (assignment,
// e.g. a named segment's `=` form) still lexes as ASSIGN.
func TestRouteArrow(t *testing.T) {
	assertTokens(t, `.region == "EU" => eu_sink`, []tok{
		{DOT, ""}, {IDENT, "region"}, {EQ, ""}, {STRING, "EU"}, {ARROW, ""}, {IDENT, "eu_sink"},
		{EOF, ""},
	})
	assertTokens(t, "pipeline clean =", []tok{
		{PIPELINE, ""}, {IDENT, "clean"}, {ASSIGN, ""},
		{EOF, ""},
	})
}

// TestRecordSpreadEllipsis checks "..." lexes as one ELLIPSIS token, not
// three DOTs, and that a lone "." next to it still lexes as DOT.
func TestRecordSpreadEllipsis(t *testing.T) {
	assertTokens(t, "{ ...row, name: .name }", []tok{
		{LBRACE, ""}, {ELLIPSIS, ""}, {IDENT, "row"}, {COMMA, ""},
		{IDENT, "name"}, {COLON, ""}, {DOT, ""}, {IDENT, "name"}, {RBRACE, ""},
		{EOF, ""},
	})
}

func TestOperators(t *testing.T) {
	assertTokens(t, "+ - * / < > <= >= == != && ||", []tok{
		{PLUS, ""}, {MINUS, ""}, {STAR, ""}, {SLASH, ""},
		{LT, ""}, {GT, ""}, {LE, ""}, {GE, ""}, {EQ, ""}, {NE, ""},
		{AND, ""}, {OR, ""},
		{EOF, ""},
	})
}

func TestErrorPolicyKeywords(t *testing.T) {
	assertTokens(t, "on error abort\non error skip\non error |> errors", []tok{
		{ON, ""}, {ERROR, ""}, {ABORT, ""},
		{ON, ""}, {ERROR, ""}, {SKIP, ""},
		{ON, ""}, {ERROR, ""}, {PIPE, ""}, {IDENT, "errors"},
		{EOF, ""},
	})
}

func TestNumberLiterals(t *testing.T) {
	assertTokens(t, "18 3.14 0 42", []tok{
		{INT, "18"}, {DOUBLE, "3.14"}, {INT, "0"}, {INT, "42"},
		{EOF, ""},
	})
}

func TestStringEscapes(t *testing.T) {
	assertTokens(t, `"hello\nworld" "a\"b" "tab\ttab"`, []tok{
		{STRING, "hello\nworld"}, {STRING, `a"b`}, {STRING, "tab\ttab"},
		{EOF, ""},
	})
}

func TestLineComment(t *testing.T) {
	src := `on error abort // stop the whole run
on error skip`
	assertTokens(t, src, []tok{
		{ON, ""}, {ERROR, ""}, {ABORT, ""},
		{ON, ""}, {ERROR, ""}, {SKIP, ""},
		{EOF, ""},
	})
}

func TestFuncDeclKeywordsAndBool(t *testing.T) {
	assertTokens(t, "func f(): string = true", []tok{
		{FUNC, ""}, {IDENT, "f"}, {LPAREN, ""}, {RPAREN, ""}, {COLON, ""},
		{IDENT, "string"}, {ASSIGN, ""}, {TRUE, ""},
		{EOF, ""},
	})
	assertTokens(t, "false", []tok{{FALSE, ""}, {EOF, ""}})
}

func TestUnterminatedString(t *testing.T) {
	l := New(`"never closed`)
	got := l.Next()
	if got.Kind != ILLEGAL {
		t.Fatalf("Kind = %v, want ILLEGAL", got.Kind)
	}
	if got.Lit != "unterminated string literal" {
		t.Errorf("Lit = %q", got.Lit)
	}
}

func TestUnknownEscape(t *testing.T) {
	l := New(`"bad\qescape"`)
	got := l.Next()
	if got.Kind != ILLEGAL {
		t.Fatalf("Kind = %v, want ILLEGAL", got.Kind)
	}
	want := `unknown escape sequence "\q"`
	if got.Lit != want {
		t.Errorf("Lit = %q, want %q", got.Lit, want)
	}
}

func TestIllegalCharacter(t *testing.T) {
	l := New("#")
	got := l.Next()
	if got.Kind != ILLEGAL {
		t.Fatalf("Kind = %v, want ILLEGAL", got.Kind)
	}
	want := `unexpected character '#'`
	if got.Lit != want {
		t.Errorf("Lit = %q, want %q", got.Lit, want)
	}
}

// TestPositions spot-checks line/col tracking across a newline, since
// none of the token-table tests above assert on Pos.
func TestPositions(t *testing.T) {
	l := New("in\n  out")

	tk := l.Next()
	if tk.Pos != (Pos{Line: 1, Col: 1}) {
		t.Errorf("first token Pos = %v, want 1:1", tk.Pos)
	}

	tk = l.Next()
	if tk.Kind != IDENT || tk.Lit != "out" {
		t.Fatalf("second token = %+v, want IDENT(out)", tk)
	}
	if tk.Pos != (Pos{Line: 2, Col: 3}) {
		t.Errorf("second token Pos = %v, want 2:3", tk.Pos)
	}
}

func TestEOFIsSticky(t *testing.T) {
	l := New("in")
	l.Next() // consumes "in"
	first := l.Next()
	second := l.Next()
	if first.Kind != EOF || second.Kind != EOF {
		t.Errorf("expected EOF, EOF; got %v, %v", first.Kind, second.Kind)
	}
}
