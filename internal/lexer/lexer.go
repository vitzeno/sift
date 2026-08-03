package lexer

import (
	"fmt"
	"strings"
)

// Lexer scans Sift source text into Tokens on demand. It operates on a
// []rune of the whole input rather than bytes: v0 source files are small
// ETL scripts, so paying for the upfront conversion buys simple,
// off-by-one-free indexing (each element is one character) instead of
// juggling UTF-8 byte widths while scanning.
type Lexer struct {
	src []rune
	pos int // index of the next unread rune

	line int // current line, 1-based
	col  int // current column, 1-based
}

// New returns a Lexer ready to scan src.
func New(src string) *Lexer {
	return &Lexer{
		src:  []rune(src),
		pos:  0,
		line: 1,
		col:  1,
	}
}

func (l *Lexer) peekAt(offset int) rune {
	i := l.pos + offset
	if i >= len(l.src) {
		return 0
	}
	return l.src[i]
}

func (l *Lexer) peek() rune {
	return l.peekAt(0)
}

func (l *Lexer) atEOF() bool {
	return l.pos >= len(l.src)
}

// advance consumes and returns the current rune, updating line/col.
func (l *Lexer) advance() rune {
	ch := l.src[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return ch
}

func (l *Lexer) skipWhitespaceAndComments() {
	for !l.atEOF() {
		switch {
		case l.peek() == ' ' || l.peek() == '\t' || l.peek() == '\r' || l.peek() == '\n':
			l.advance()
		case l.peek() == '/' && l.peekAt(1) == '/':
			for !l.atEOF() && l.peek() != '\n' {
				l.advance()
			}
		default:
			return
		}
	}
}

// Next scans and returns the next token. Once it returns a token with
// Kind == EOF, every subsequent call returns EOF again.
func (l *Lexer) Next() Token {
	l.skipWhitespaceAndComments()

	start := Pos{Line: l.line, Col: l.col}

	if l.atEOF() {
		return Token{Kind: EOF, Pos: start}
	}

	ch := l.peek()
	switch {
	case isLetter(ch):
		return l.scanIdent(start)
	case isDigit(ch):
		return l.scanNumber(start)
	case ch == '"':
		return l.scanString(start)
	}

	// Symbols and operators, longest match first.
	switch ch {
	case '.':
		if l.peekAt(1) == '.' && l.peekAt(2) == '.' {
			l.advance()
			l.advance()
			l.advance()
			return Token{Kind: ELLIPSIS, Pos: start}
		}
		l.advance()
		return Token{Kind: DOT, Pos: start}
	case '=':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return Token{Kind: EQ, Pos: start}
		}
		return Token{Kind: ASSIGN, Pos: start}
	case '|':
		l.advance()
		if l.peek() == '>' {
			l.advance()
			return Token{Kind: PIPE, Pos: start}
		}
		if l.peek() == '|' {
			l.advance()
			return Token{Kind: OR, Pos: start}
		}
		return l.illegal(start, ch)
	case '&':
		l.advance()
		if l.peek() == '&' {
			l.advance()
			return Token{Kind: AND, Pos: start}
		}
		return l.illegal(start, ch)
	case '<':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return Token{Kind: LE, Pos: start}
		}
		return Token{Kind: LT, Pos: start}
	case '>':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return Token{Kind: GE, Pos: start}
		}
		return Token{Kind: GT, Pos: start}
	case '!':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return Token{Kind: NE, Pos: start}
		}
		return l.illegal(start, ch)
	case '{':
		l.advance()
		return Token{Kind: LBRACE, Pos: start}
	case '}':
		l.advance()
		return Token{Kind: RBRACE, Pos: start}
	case '(':
		l.advance()
		return Token{Kind: LPAREN, Pos: start}
	case ')':
		l.advance()
		return Token{Kind: RPAREN, Pos: start}
	case ':':
		l.advance()
		return Token{Kind: COLON, Pos: start}
	case ',':
		l.advance()
		return Token{Kind: COMMA, Pos: start}
	case '@':
		l.advance()
		return Token{Kind: AT, Pos: start}
	case '+':
		l.advance()
		return Token{Kind: PLUS, Pos: start}
	case '-':
		l.advance()
		return Token{Kind: MINUS, Pos: start}
	case '*':
		l.advance()
		return Token{Kind: STAR, Pos: start}
	case '/':
		l.advance()
		return Token{Kind: SLASH, Pos: start}
	}

	l.advance()
	return l.illegal(start, ch)
}

func (l *Lexer) illegal(pos Pos, ch rune) Token {
	return Token{Kind: ILLEGAL, Lit: fmt.Sprintf("unexpected character %q", ch), Pos: pos}
}

func (l *Lexer) scanIdent(start Pos) Token {
	var b strings.Builder
	for !l.atEOF() && isLetterOrDigit(l.peek()) {
		b.WriteRune(l.advance())
	}
	lit := b.String()
	if kind, ok := keywords[lit]; ok {
		return Token{Kind: kind, Pos: start}
	}
	return Token{Kind: IDENT, Lit: lit, Pos: start}
}

// scanNumber scans an INT, or a DOUBLE if a '.' is followed by at least
// one digit. A '.' not followed by a digit is left unconsumed — v0 has
// no fields or methods on numbers, so it can only be a lexer error at the
// next call, and leaving it alone keeps this function's job to just
// "read a number."
//
// decision: no leading-dot doubles (".5") and no exponents ("1e9") — v0's
// literal grammar doesn't call for them, and adding them now would be
// unused surface area.
func (l *Lexer) scanNumber(start Pos) Token {
	var b strings.Builder
	for !l.atEOF() && isDigit(l.peek()) {
		b.WriteRune(l.advance())
	}
	if l.peek() == '.' && isDigit(l.peekAt(1)) {
		b.WriteRune(l.advance()) // '.'
		for !l.atEOF() && isDigit(l.peek()) {
			b.WriteRune(l.advance())
		}
		return Token{Kind: DOUBLE, Lit: b.String(), Pos: start}
	}
	return Token{Kind: INT, Lit: b.String(), Pos: start}
}

// scanString scans a double-quoted string literal, unescaping as it
// goes. Supported escapes: \" \\ \n \t \r. An unterminated literal or an
// unknown escape produces ILLEGAL rather than panicking — these are real
// user typos in a .sift file, not internal invariant violations
// (CLAUDE.md: "no bare panic on user-facing error paths").
func (l *Lexer) scanString(start Pos) Token {
	l.advance() // opening quote

	var b strings.Builder
	for {
		if l.atEOF() {
			return Token{Kind: ILLEGAL, Lit: "unterminated string literal", Pos: start}
		}
		ch := l.advance()
		if ch == '"' {
			return Token{Kind: STRING, Lit: b.String(), Pos: start}
		}
		if ch != '\\' {
			b.WriteRune(ch)
			continue
		}
		if l.atEOF() {
			return Token{Kind: ILLEGAL, Lit: "unterminated string literal", Pos: start}
		}
		esc := l.advance()
		switch esc {
		case '"':
			b.WriteRune('"')
		case '\\':
			b.WriteRune('\\')
		case 'n':
			b.WriteRune('\n')
		case 't':
			b.WriteRune('\t')
		case 'r':
			b.WriteRune('\r')
		default:
			// Not %q on "\"+string(esc): that would double the
			// backslash (Go's string-literal escaping) and show the
			// user "\\q" for input they wrote as "\q".
			return Token{Kind: ILLEGAL, Lit: fmt.Sprintf("unknown escape sequence \"\\%c\"", esc), Pos: start}
		}
	}
}

func isLetter(ch rune) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isDigit(ch rune) bool {
	return ch >= '0' && ch <= '9'
}

func isLetterOrDigit(ch rune) bool {
	return isLetter(ch) || isDigit(ch)
}
