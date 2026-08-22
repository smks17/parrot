package parser

type TokenKind int

const (
	Word        TokenKind = iota
	Pipe                  // |
	Redirect              // >
	Append                // >>
	And                   // &&
	SingleQuote           // '
	DoubleQuote           // "
	Backslash             // \
)

type Token struct {
	Kind  TokenKind
	Value string // for Word, the literal text; for operators, unused
}

func NewToken(kind TokenKind, value string) *Token {
	return &Token{kind, value}
}
