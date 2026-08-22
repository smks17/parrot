package commands

import (
	"fmt"
	"io"
	"strings"
)

type Echo struct{}

var _ Command = Echo{}

func (echo Echo) Name() string {
	return "echo"
}

func (echo Echo) Usage() string { return "echo text...  — print text" }

func (echo Echo) Run(ctx *Context, args []string) int {
	if len(args) == 0 {
		if ctx.Stdin == nil {
			fmt.Fprintf(ctx.Stderr, "%s: missing operand\n", echo.Name())
			return 1
		}
		if _, err := io.Copy(ctx.Stdout, ctx.Stdin); err != nil {
			fmt.Fprintf(ctx.Stderr, "%s: %v\n", echo.Name(), err)
			return 1
		}
		return 0
	}

	content := strings.Join(args, " ") + "\n"
	ctx.Stdout.Write([]byte(content))
	return 0
}

func init() { Register(Echo{}) }
