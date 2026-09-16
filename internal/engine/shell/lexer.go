package shell

import (
	"fmt"
	"strings"
)

type Quote int

const (
	Bare Quote = iota // no quote
	Double
	Single
)

type Piece struct {
	Text  string
	Quote Quote
}

type Word []Piece

type Token struct {
	Op   string
	Word Word
}

func (t Token) IsWord() bool { return t.Op == "" }

var operators = []string{"2>&1", ">&2", "2>>", "2>", ">>", "&&", "||", "\n", ";", "|", "&", "<", ">", "(", ")"}

const wordEnd = " \t\r\n;|&<>()"

func Lex(src string) ([]Token, error) {
	var tokens []Token
	for i := 0; i < len(src); {
		switch {
		case src[i] == ' ' || src[i] == '\t' || src[i] == '\r':
			i++
		case src[i] == '#': // a comment runs to the end of the line
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case src[i] == '\\' && i+1 < len(src) && src[i+1] == '\n':
			i += 2 // a backslash before a newline joins the two lines
		default:
			if op := matchOperator(src[i:]); op != "" {
				tokens = append(tokens, Token{Op: op})
				i += len(op)
				continue
			}
			word, next, err := lexWord(src, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, Token{Word: word})
			i = next
		}
	}
	return tokens, nil
}

func matchOperator(src string) string {
	for _, op := range operators {
		if strings.HasPrefix(src, op) {
			return op
		}
	}
	return ""
}

func lexWord(src string, i int) (Word, int, error) {
	var word Word
	var text strings.Builder
	// flush stores the bare text collected so far as one piece.
	flush := func() {
		if text.Len() > 0 {
			word = append(word, Piece{Text: text.String(), Quote: Bare})
			text.Reset()
		}
	}
	for i < len(src) && !strings.ContainsRune(wordEnd, rune(src[i])) {
		switch src[i] {
		case '\'':
			end := strings.IndexByte(src[i+1:], '\'')
			if end < 0 {
				return nil, 0, fmt.Errorf("unmatched '")
			}
			flush()
			word = append(word, Piece{Text: src[i+1 : i+1+end], Quote: Single})
			i += end + 2
		case '"':
			inner, next, err := lexDoubleQuote(src, i)
			if err != nil {
				return nil, 0, err
			}
			flush()
			word = append(word, Piece{Text: inner, Quote: Double})
			i = next
		case '\\':
			if i+1 >= len(src) {
				text.WriteByte('\\')
				i++
				break
			}
			// An escaped character is literal, so it becomes a single-quoted
			// piece of its own: "a\ b" is one word with a real space in it.
			flush()
			word = append(word, Piece{Text: string(src[i+1]), Quote: Single})
			i += 2
		case '$':
			// The whole $... construct is copied as written.
			end := scanDollar(src, i)
			text.WriteString(src[i:end])
			i = end
		default:
			text.WriteByte(src[i])
			i++
		}
	}
	flush()
	return word, i, nil
}

func lexDoubleQuote(src string, i int) (string, int, error) {
	var text strings.Builder
	for i++; i < len(src); {
		switch {
		case src[i] == '"':
			return text.String(), i + 1, nil
		case src[i] == '\\' && i+1 < len(src) && strings.IndexByte(`"\$`, src[i+1]) >= 0:
			// Inside double quotes a backslash only escapes these three; in
			// front of anything else it is an ordinary character.
			text.WriteByte(src[i+1])
			i += 2
		case src[i] == '$':
			end := scanDollar(src, i)
			text.WriteString(src[i:end])
			i = end
		default:
			text.WriteByte(src[i])
			i++
		}
	}
	return "", 0, fmt.Errorf(`unmatched "`)
}

// scanDollar returns the index just past the $... construct starting at i.
func scanDollar(src string, i int) int {
	i++ // the $
	if i >= len(src) {
		return i
	}
	switch {
	case src[i] == '(': // $(command) and $((arithmetic)), which may nest
		depth := 0
		for ; i < len(src); i++ {
			switch src[i] {
			case '(':
				depth++
			case ')':
				if depth--; depth == 0 {
					return i + 1
				}
			}
		}
		return i
	case src[i] == '{': // ${name} and ${name:-default}
		if end := strings.IndexByte(src[i:], '}'); end >= 0 {
			return i + end + 1
		}
		return len(src)
	case isDigit(src[i]): // $1 is one digit: $12 is $1 followed by a 2
		return i + 1
	case isNameChar(src[i]):
		for i < len(src) && isNameChar(src[i]) {
			i++
		}
		return i
	}
	return i + 1 // a one-character name such as $? or $#
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isNameChar(c byte) bool {
	return c == '_' || isDigit(c) || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isName(s string) bool {
	if s == "" || isDigit(s[0]) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isNameChar(s[i]) {
			return false
		}
	}
	return true
}
