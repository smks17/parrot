package shell

import (
	"errors"
	"fmt"
	"strings"
)

// The tree. A Cmd is any one of the types below;
type Cmd any

// List is commands run one after another, written with ";" or newlines.
type List struct {
	Cmds []Cmd
}

// AndOr runs Right only when Left succeeded ("&&") or failed ("||").
type AndOr struct {
	Left  Cmd
	Op    string
	Right Cmd
}

// Pipeline feeds each command's output into the next.
type Pipeline struct {
	Cmds []Cmd
}

// Simple is a plain command: "x=1 grep -n foo file > out".
type Simple struct {
	Assigns []Assign
	Words   []Word
	Redirs  []Redirect
}

type Assign struct {
	Name  string
	Value Word
}

type Redirect struct {
	Op     string
	Target Word
}

type If struct {
	Cond *List
	Then *List
	Else *List // nil when there is no else
}

type Loop struct {
	Cond  *List
	Body  *List
	Until bool
}

type For struct {
	Name  string
	Items []Word
	Body  *List
}

// type FuncDef struct {
// 	Name string
// 	Body *List
// }

// eof is a sentinel operator at the end of the tokens, so that the parser can
// always look at a token without checking how many are left.
const eof = "\x00"

func Parse(src string) (*List, error) {
	tokens, err := Lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: append(tokens, Token{Op: eof})}

	list, err := p.list()
	if err != nil {
		return nil, err
	}
	if !p.isOp(eof) {
		return nil, p.unexpected()
	}
	return list, nil
}

type parser struct {
	tokens []Token
	pos    int
}

func (p *parser) tok() Token { return p.tokens[p.pos] }

func (p *parser) next() Token {
	token := p.tok()
	if p.pos < len(p.tokens)-1 {
		p.pos++
	}
	return token
}

func (p *parser) isOp(op string) bool { return p.tok().Op == op }

func (p *parser) isWord(text string) bool {
	word := p.tok().Word
	return p.tok().IsWord() && len(word) == 1 && word[0].Quote == Bare && word[0].Text == text
}

// keyword consumes an expected keyword.
func (p *parser) consume(text string) error {
	if !p.isWord(text) {
		return fmt.Errorf("syntax error: expected %q near %s", text, p.getError().Error())
	}
	p.next()
	return nil
}

func (p *parser) skipSeparators() {
	for p.isOp("\n") || p.isOp(";") {
		p.next()
	}
}

// wordText is the word as written, ignoring quoting. It is only for messages.
func wordText(w Word) string {
	text := ""
	for _, piece := range w {
		text += piece.Text
	}
	return text
}

func (p *parser) list(stops ...string) (*List, error) {
	list := &List{}
	for {
		p.skipSeparators()
		if p.stopped(stops) {
			return list, nil
		}
		cmd, err := p.andOr()
		if err != nil {
			return nil, err
		}
		list.Cmds = append(list.Cmds, cmd)
		if !p.isOp(";") && !p.isOp("\n") {
			return list, nil // the caller decides whether this is the end
		}
	}
}

func (p *parser) stopped(stops []string) bool {
	if p.isOp(eof) || p.isOp(")") {
		return true
	}
	for _, stop := range stops {
		if p.isWord(stop) {
			return true
		}
	}
	return false
}

func (p *parser) andOr() (Cmd, error) {
	left, err := p.pipeline()
	if err != nil {
		return nil, err
	}
	for p.isOp("&&") || p.isOp("||") {
		op := p.next().Op
		p.skipNewlines()
		right, err := p.pipeline()
		if err != nil {
			return nil, err
		}
		left = &AndOr{Left: left, Op: op, Right: right}
	}
	return left, nil
}

func (p *parser) skipNewlines() {
	for p.isOp("\n") {
		p.next()
	}
}

func (p *parser) pipeline() (Cmd, error) {
	cmd, err := p.command()
	if err != nil {
		return nil, err
	}
	if !p.isOp("|") {
		return cmd, nil // no pipe, no wrapper
	}

	pipeline := &Pipeline{Cmds: []Cmd{cmd}}
	for p.isOp("|") {
		p.next()
		p.skipNewlines()
		cmd, err := p.command()
		if err != nil {
			return nil, err
		}
		pipeline.Cmds = append(pipeline.Cmds, cmd)
	}
	return pipeline, nil
}

func (p *parser) command() (Cmd, error) {
	switch {
	case p.isWord("if"):
		return p.ifCmd()
	case p.isWord("while"):
		return p.loop(false)
	case p.isWord("until"):
		return p.loop(true)
	case p.isWord("for"):
		return p.forCmd()
	case p.isFuncDef():
		return p.funcDef()
	}
	return p.simple()
}

// ifCmd parses a whole "if ... fi".
func (p *parser) ifCmd() (Cmd, error) {
	p.next() // if
	cmd, err := p.ifTail()
	if err != nil {
		return nil, err
	}
	return cmd, p.consume("fi")
}

// ifTail parses everything between "if" and the closing "fi". An "elif" is
// parsed by calling this again: the single "fi" closes all of them.
func (p *parser) ifTail() (*If, error) {
	cond, err := p.list("then")
	if err != nil {
		return nil, err
	}
	if err := p.consume("then"); err != nil {
		return nil, err
	}
	then, err := p.list("elif", "else", "fi")
	if err != nil {
		return nil, err
	}

	cmd := &If{Cond: cond, Then: then}
	switch {
	case p.isWord("elif"):
		p.next()
		nested, err := p.ifTail()
		if err != nil {
			return nil, err
		}
		cmd.Else = &List{Cmds: []Cmd{nested}}
	case p.isWord("else"):
		p.next()
		if cmd.Else, err = p.list("fi"); err != nil {
			return nil, err
		}
	}
	return cmd, nil
}

func (p *parser) loop(until bool) (Cmd, error) {
	p.next() // while | until
	cond, err := p.list("do")
	if err != nil {
		return nil, err
	}
	body, err := p.doBody()
	if err != nil {
		return nil, err
	}
	return &Loop{Cond: cond, Body: body, Until: until}, nil
}

func (p *parser) forCmd() (Cmd, error) {
	p.next() // for
	if !p.tok().IsWord() {
		return nil, p.unexpected()
	}
	name := wordText(p.next().Word)
	if !isName(name) {
		return nil, fmt.Errorf("syntax error: %q is not a variable name", name)
	}

	cmd := &For{Name: name}
	if p.isWord("in") {
		p.next()
		for p.tok().IsWord() && !p.isWord("do") {
			cmd.Items = append(cmd.Items, p.next().Word)
		}
	} else {
		// "for x" with no list walks the arguments the script was given.
		cmd.Items = []Word{{{Text: "$@", Quote: Double}}}
	}

	body, err := p.doBody()
	if err != nil {
		return nil, err
	}
	cmd.Body = body
	return cmd, nil
}

// doBody parses the "do ... done" that every loop ends with.
func (p *parser) doBody() (*List, error) {
	p.skipSeparators()
	if err := p.consume("do"); err != nil {
		return nil, err
	}
	body, err := p.list("done")
	if err != nil {
		return nil, err
	}
	return body, p.consume("done")
}

// isFuncDef reports whether the next tokens are "name ( )".
func (p *parser) isFuncDef() bool {
	return p.tok().IsWord() && p.pos+2 < len(p.tokens) &&
		p.tokens[p.pos+1].Op == "(" && p.tokens[p.pos+2].Op == ")"
}

func (p *parser) funcDef() (Cmd, error) {
	return nil, errors.New("not implemented") // TODO: implement

}

var redirects = map[string]bool{">": true, ">>": true, "<": true, "2>": true, "2>>": true, "2>&1": true}

func (p *parser) simple() (Cmd, error) {
	cmd := &Simple{}
	for {
		if redirects[p.tok().Op] {
			redirect := Redirect{Op: p.next().Op}
			if redirect.Op != "2>&1" { // this one names a stream, not a file
				if !p.tok().IsWord() {
					return nil, p.unexpected()
				}
				redirect.Target = p.next().Word
			}
			cmd.Redirs = append(cmd.Redirs, redirect)
		} else if p.tok().IsWord() {
			word := p.next().Word
			if len(cmd.Words) == 0 {
				if assign, ok := splitAssign(word); ok {
					cmd.Assigns = append(cmd.Assigns, assign)
					continue
				}
			}
			cmd.Words = append(cmd.Words, word)
		} else {
			if len(cmd.Words) == 0 && len(cmd.Assigns) == 0 && len(cmd.Redirs) == 0 {
				return nil, p.unexpected()
			}
			return cmd, nil
		}
	}
}

func splitAssign(word Word) (Assign, bool) {
	if len(word) == 0 || word[0].Quote != Bare {
		return Assign{}, false
	}
	name, rest, found := strings.Cut(word[0].Text, "=")
	if !found || !isName(name) {
		return Assign{}, false
	}
	value := Word{}
	if rest != "" {
		value = append(value, Piece{Text: rest, Quote: Bare})
	}
	value = append(value, word[1:]...)
	return Assign{Name: name, Value: value}, true
}
