package commands

import (
	"fmt"
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
		fmt.Fprintf(ctx.Stderr, "%s: missing operand\n", echo.Name())
		return 1
	}

	content := strings.Join(args, " ") + "\n"
	ctx.Stdout.Write([]byte(content))
	return 0
}

func init() { Register(Echo{}) }
