package parser

import (
	"fmt"
	"strings"
)

type lexState int

const (
	stateDefault lexState = iota
	stateSingleQuote
	stateDoubleQuote
	stateEscaped
)

func Parse(line string) ([]*Token, error) {
	var tokens []*Token
	var word strings.Builder
	hasWord := false
	state := stateDefault
	prevState := stateDefault // where Escaped returns to once it fires

	flushWord := func() {
		if hasWord {
			tokens = append(tokens, NewToken(Word, word.String()))
			word.Reset()
			hasWord = false
		}
	}

	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		switch state {
		case stateEscaped:
			word.WriteRune(r)
			hasWord = true
			state = prevState
			continue

		case stateSingleQuote:
			if r == '\'' {
				state = stateDefault
			} else {
				word.WriteRune(r)
				hasWord = true
			}
			continue

		case stateDoubleQuote:
			if r == '"' {
				state = stateDefault
			} else if r == '\\' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\') {
				word.WriteRune(runes[i+1])
				hasWord = true
				i++
			} else {
				word.WriteRune(r)
				hasWord = true
			}
			continue
		}

		// state == stateDefault from here on.
		switch {
		case r == '\'':
			state = stateSingleQuote
			hasWord = true
		case r == '"':
			state = stateDoubleQuote
			hasWord = true
		case r == '\\':
			prevState = stateDefault
			state = stateEscaped
		case r == ' ' || r == '\t':
			flushWord()
		case r == '>':
			flushWord()
			if i+1 < len(runes) && runes[i+1] == '>' {
				tokens = append(tokens, NewToken(Append, ">>"))
				i++
			} else {
				tokens = append(tokens, NewToken(Redirect, ">"))
			}
		case r == '|':
			flushWord()
			tokens = append(tokens, NewToken(Pipe, "|"))
		case r == '&' && i+1 < len(runes) && runes[i+1] == '&':
			flushWord()
			tokens = append(tokens, NewToken(And, "&&"))
			i++
		default:
			word.WriteRune(r)
			hasWord = true
		}
	}

	if state == stateSingleQuote || state == stateDoubleQuote {
		return nil, fmt.Errorf("unexpected EOF while looking for matching quote")
	}
	if state == stateEscaped {
		word.WriteByte('\\')
		hasWord = true
	}

	flushWord()
	return tokens, nil
}
