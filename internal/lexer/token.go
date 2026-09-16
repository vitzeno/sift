// Package lexer turns Sift source text into a stream of Tokens, each
// carrying its source position so the parser and checker can produce
// diagnostics that point at real code.
package lexer

import "fmt"

// Kind classifies a Token.
type Kind int

const (
	EOF Kind = iota
	ILLEGAL

	IDENT
	INT
	DOUBLE
	STRING

	// keywords
	SOURCE
	SINK
	PIPELINE
	ON
	ERROR
	ABORT
	SKIP
	FUNC
	TRUE
	FALSE

	// symbols
	ASSIGN   // =
	PIPE     // |>
	LBRACE   // {
	RBRACE   // }
	LPAREN   // (
	RPAREN   // )
	COLON    // :
	COMMA    // ,
	DOT      // .
	ELLIPSIS // ...
	AT       // @
	QUESTION // ?
	COALESCE // ??
	ARROW    // =>

	PLUS  // +
	MINUS // -
	STAR  // *
	SLASH // /

	LT // <
	GT // >
	LE // <=
	GE // >=
	EQ // ==
	NE // !=

	AND // &&
	OR  // ||
)

var kindNames = map[Kind]string{
	EOF:      "EOF",
	ILLEGAL:  "ILLEGAL",
	IDENT:    "IDENT",
	INT:      "INT",
	DOUBLE:   "DOUBLE",
	STRING:   "STRING",
	SOURCE:   "SOURCE",
	SINK:     "SINK",
	PIPELINE: "PIPELINE",
	ON:       "ON",
	ERROR:    "ERROR",
	ABORT:    "ABORT",
	SKIP:     "SKIP",
	FUNC:     "FUNC",
	TRUE:     "TRUE",
	FALSE:    "FALSE",
	ASSIGN:   "ASSIGN",
	PIPE:     "PIPE",
	LBRACE:   "LBRACE",
	RBRACE:   "RBRACE",
	LPAREN:   "LPAREN",
	RPAREN:   "RPAREN",
	COLON:    "COLON",
	COMMA:    "COMMA",
	DOT:      "DOT",
	ELLIPSIS: "ELLIPSIS",
	AT:       "AT",
	QUESTION: "QUESTION",
	COALESCE: "COALESCE",
	ARROW:    "ARROW",
	PLUS:     "PLUS",
	MINUS:    "MINUS",
	STAR:     "STAR",
	SLASH:    "SLASH",
	LT:       "LT",
	GT:       "GT",
	LE:       "LE",
	GE:       "GE",
	EQ:       "EQ",
	NE:       "NE",
	AND:      "AND",
	OR:       "OR",
}

func (k Kind) String() string {
	if name, ok := kindNames[k]; ok {
		return name
	}
	return "UNKNOWN"
}

var opSymbols = map[Kind]string{
	PLUS:     "+",
	MINUS:    "-",
	STAR:     "*",
	SLASH:    "/",
	LT:       "<",
	GT:       ">",
	LE:       "<=",
	GE:       ">=",
	EQ:       "==",
	NE:       "!=",
	AND:      "&&",
	OR:       "||",
	COALESCE: "??",
}

// Symbol renders a binary operator's surface syntax ("+", ">=", ...)
// rather than its debug name ("PLUS", "GE"), for output meant to read
// like the .sift source a user typed. Checker diagnostics and --emit-ast
// both need this, so it lives here instead of being duplicated in each.
func (k Kind) Symbol() string {
	if sym, ok := opSymbols[k]; ok {
		return sym
	}
	return k.String()
}

// keywords is the complete, closed set of reserved words. Stage names
// (filter/map/check), format names (csv/jsonl), and function names
// (mask/hash/upper/...) are absent on purpose: they're ordinary
// identifiers that the parser and checker resolve, not lexer-level
// keywords, the same "resolve late, don't special-case early" shape the
// format registry uses.
var keywords = map[string]Kind{
	"source":   SOURCE,
	"sink":     SINK,
	"pipeline": PIPELINE,
	"on":       ON,
	"error":    ERROR,
	"abort":    ABORT,
	"skip":     SKIP,
	"func":     FUNC,
	"true":     TRUE,
	"false":    FALSE,
}

// Pos is a source position. It lives here, in the lexer, since that's
// where positions are first produced. The parser and checker (modules
// 5-6) reuse this type directly instead of each defining their own.
type Pos struct {
	Line int
	Col  int
}

func (p Pos) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Col)
}

// Token is one lexical unit. Lit holds the literal text for IDENT/INT/
// DOUBLE, the unescaped contents for STRING, and a human-readable message
// for ILLEGAL. It is empty for fixed-text tokens (keywords, symbols).
type Token struct {
	Kind Kind
	Lit  string
	Pos  Pos
}

func (t Token) String() string {
	if t.Lit == "" {
		return t.Kind.String()
	}
	return fmt.Sprintf("%s(%q)", t.Kind, t.Lit)
}
