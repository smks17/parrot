package shell

import (
	"errors"
	"fmt"
)

func (p *parser) getError() error {
	switch {
	case p.isOp(eof):
		return errors.New("end of input")
	case p.isOp("\n"):
		return errors.New("newline")
	case p.tok().IsWord():
		return errors.New(fmt.Sprintf("%q", wordText(p.tok().Word)))
	}
	return errors.New(fmt.Sprintf("%q", p.tok().Op))
}

func (p *parser) unexpected() error {
	return fmt.Errorf("syntax error near %s", p.getError().Error())
}
