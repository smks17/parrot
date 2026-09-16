package shell

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// fields collects the strings a word expands to. One is being built at any
// moment; a blank in unquoted text finishes it and starts the next.
type fields struct {
	all     []string // the ones finished so far
	current string   // the one being built
	started bool     // whether current counts, even while it is empty
}

func (f *fields) add(text string) {
	f.current += text
	f.started = true
}

// finish ends the field being built, if there is one.
func (f *fields) finish() {
	if f.started {
		f.all = append(f.all, f.current)
		f.current, f.started = "", false
	}
}

func (f *fields) addSplit(text string) {
	for i := 0; i < len(text); {
		if isBlank(text[i]) {
			f.finish()
			for i < len(text) && isBlank(text[i]) {
				i++
			}
			continue
		}
		start := i
		for i < len(text) && !isBlank(text[i]) {
			i++
		}
		f.add(text[start:i])
	}
}

func (f *fields) result() []string {
	f.finish()
	return f.all
}

func (sh *Shell) expandWords(words []Word, io Streams) ([]string, error) {
	var pieces []string
	for _, word := range words {
		expanded, err := sh.expandWord(word, io)
		if err != nil {
			return nil, err
		}
		pieces = append(pieces, expanded...)
	}
	return pieces, nil
}

func (sh *Shell) expandWord(word Word, io Streams) ([]string, error) {
	if len(word) == 1 && word[0].Quote == Double && word[0].Text == "$@" {
		return append([]string(nil), sh.params...), nil
	}

	var built fields
	for _, piece := range word {
		// Single quotes mean the text is used as written.
		if piece.Quote == Single {
			built.add(piece.Text)
			continue
		}

		text, err := sh.expandText(piece.Text, io)
		if err != nil {
			return nil, err
		}
		if piece.Quote == Double {
			built.add(text) // quoted: whatever came out stays one field
			continue
		}
		built.addSplit(text) // bare: blanks inside it separate fields
	}

	// What came out may name files: "*.txt" becomes the files it matches.
	return sh.matchFiles(word, built.result()), nil
}

// expandOne expands a word that cannot become more than one string: the value
// of an assignment, or the file a redirection names.
func (sh *Shell) expandOne(word Word, io Streams) (string, error) {
	fields, err := sh.expandWord(word, io)
	if err != nil {
		return "", err
	}
	return strings.Join(fields, " "), nil
}

// expandText replaces every $... in a piece of text.
func (sh *Shell) expandText(text string, io Streams) (string, error) {
	var out strings.Builder
	for i := 0; i < len(text); {
		if text[i] != '$' {
			out.WriteByte(text[i])
			i++
			continue
		}
		// scanDollar, in lexer.go, already knows where a $... ends.
		end := scanDollar(text, i)
		value, err := sh.expandDollar(text[i:end], io)
		if err != nil {
			return "", err
		}
		out.WriteString(value)
		i = end
	}
	return out.String(), nil
}

// expandDollar takes one whole $... and returns what it stands for.
func (sh *Shell) expandDollar(src string, io Streams) (string, error) {
	body := src[1:] // drop the $
	switch {
	case strings.HasPrefix(body, "(("): // $((1 + 2))
		value, err := sh.arith(strings.TrimSuffix(body[2:], "))"), io)
		if err != nil {
			return "", err
		}
		return strconv.Itoa(value), nil

	case strings.HasPrefix(body, "("): // $(echo hi)
		var out bytes.Buffer
		sh.Run(trimEnds(body), Streams{In: strings.NewReader(""), Out: &out, Err: io.Err})
		return strings.TrimRight(out.String(), "\n"), nil

	case strings.HasPrefix(body, "{"): // ${name}, ${name:-default}, ${#name}
		return sh.expandBrace(trimEnds(body), io)
	}
	return sh.variable(body), nil
}

func trimEnds(s string) string {
	if len(s) < 2 {
		return ""
	}
	return s[1 : len(s)-1]
}

func (sh *Shell) expandBrace(inner string, io Streams) (string, error) {
	if name, ok := strings.CutPrefix(inner, "#"); ok {
		return strconv.Itoa(len(sh.variable(name))), nil // ${#name} is a length
	}
	if name, fallback, ok := strings.Cut(inner, ":-"); ok {
		if value := sh.variable(name); value != "" {
			return value, nil
		}
		return sh.expandText(fallback, io) // ${name:-default}
	}

	// Anything else — ${name#pattern} and the rest of the family — is not
	// understood here, and saying so is better than quietly expanding to
	// nothing, which looks like an empty variable.
	if !isName(inner) && !isSpecial(inner) {
		return "", fmt.Errorf("bad substitution: ${%s}", inner)
	}
	return sh.variable(inner), nil
}

// isSpecial reports whether a name is one of the shell's own: $?, $#, $1.
func isSpecial(name string) bool {
	if len(name) == 1 && strings.ContainsAny(name, "?#@*$!-") {
		return true
	}
	_, err := strconv.Atoi(name)
	return err == nil
}

// variable reads a variable, an argument, or one of the shell's own values.
func (sh *Shell) variable(name string) string {
	switch name {
	case "?":
		return strconv.Itoa(sh.status)
	case "#":
		return strconv.Itoa(len(sh.params))
	case "@", "*":
		return strings.Join(sh.params, " ")
	case "0":
		return "prt"
	}
	if n, err := strconv.Atoi(name); err == nil { // $1, $2, ...
		if n >= 1 && n <= len(sh.params) {
			return sh.params[n-1]
		}
		return ""
	}
	return sh.vars[name]
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' || c == '\n' }

var precedence = map[string]int{
	"||": 1,
	"&&": 2,
	"==": 3, "!=": 3,
	"<": 4, "<=": 4, ">": 4, ">=": 4,
	"+": 5, "-": 5,
	"*": 6, "/": 6, "%": 6,
}

func (sh *Shell) arith(src string, io Streams) (int, error) {
	// $1 and $x are replaced first, so the rest of this file only has to
	// know about numbers, plain names and operators.
	src, err := sh.expandText(src, io)
	if err != nil {
		return 0, err
	}

	p := &arithParser{sh: sh, tokens: arithTokens(src)}

	value, err := p.expr(1)
	if err != nil {
		return 0, err
	}
	if p.pos < len(p.tokens) {
		return 0, fmt.Errorf("bad arithmetic near %q", p.tokens[p.pos])
	}
	return value, nil
}

// arithTokens splits an expression into numbers, names and operators.
func arithTokens(src string) []string {
	var tokens []string
	for i := 0; i < len(src); {
		switch c := src[i]; {
		case c == ' ' || c == '\t' || c == '\n':
			i++

		case isDigit(c) || isNameChar(c):
			start := i
			for i < len(src) && isNameChar(src[i]) {
				i++
			}
			tokens = append(tokens, src[start:i])

		default:
			// Two-character operators first, so that "<=" is not read as "<".
			if i+1 < len(src) && precedence[src[i:i+2]] > 0 {
				tokens = append(tokens, src[i:i+2])
				i += 2
				continue
			}
			tokens = append(tokens, string(c))
			i++
		}
	}
	return tokens
}

type arithParser struct {
	sh     *Shell
	tokens []string
	pos    int
}

func (p *arithParser) peek() string {
	if p.pos >= len(p.tokens) {
		return ""
	}
	return p.tokens[p.pos]
}

// expr reads operators that bind at least as tightly as min, so each call
// handles one level of precedence and recursion handles the rest.
func (p *arithParser) expr(min int) (int, error) {
	left, err := p.operand()
	if err != nil {
		return 0, err
	}

	for {
		op := p.peek()
		level := precedence[op]
		if level < min {
			return left, nil
		}
		p.pos++
		right, err := p.expr(level + 1)
		if err != nil {
			return 0, err
		}
		if left, err = applyOp(op, left, right); err != nil {
			return 0, err
		}
	}
}

func (p *arithParser) operand() (int, error) {
	token := p.peek()
	p.pos++

	switch token {
	case "":
		return 0, fmt.Errorf("arithmetic expression ended too early")
	case "-":
		value, err := p.operand()
		return -value, err
	case "+":
		return p.operand()
	case "!":
		value, err := p.operand()
		return boolean(value == 0), err
	case "(":
		value, err := p.expr(1)
		if err != nil {
			return 0, err
		}
		if p.peek() != ")" {
			return 0, fmt.Errorf("missing )")
		}
		p.pos++
		return value, nil
	}

	if number, err := strconv.Atoi(token); err == nil {
		return number, nil
	}
	if isName(token) {
		// An unset variable, or one holding something else, counts as 0.
		number, _ := strconv.Atoi(strings.TrimSpace(p.sh.variable(token)))
		return number, nil
	}
	return 0, fmt.Errorf("bad arithmetic near %q", token)
}

func applyOp(op string, left, right int) (int, error) {
	switch op {
	case "+":
		return left + right, nil
	case "-":
		return left - right, nil
	case "*":
		return left * right, nil
	case "/", "%":
		if right == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		if op == "/" {
			return left / right, nil
		}
		return left % right, nil
	case "==":
		return boolean(left == right), nil
	case "!=":
		return boolean(left != right), nil
	case "<":
		return boolean(left < right), nil
	case "<=":
		return boolean(left <= right), nil
	case ">":
		return boolean(left > right), nil
	case ">=":
		return boolean(left >= right), nil
	case "&&":
		return boolean(left != 0 && right != 0), nil
	case "||":
		return boolean(left != 0 || right != 0), nil
	}
	return 0, fmt.Errorf("unknown operator %q", op)
}

func boolean(yes bool) int {
	if yes {
		return 1
	}
	return 0
}
