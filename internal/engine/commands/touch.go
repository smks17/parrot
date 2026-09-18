package commands

import (
	"fmt"
)

type Touch struct{}

var _ Command = Touch{}

func (touch Touch) Name() string {
	return "touch"
}

func (touch Touch) Usage() string {
	return "touch file...  — create files, or update their timestamps"
}

func (touch Touch) Run(ctx *Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(ctx.Stderr, "%s: missing file operand\n", touch.Name())
		return 1
	}

	exit := 0
	for _, arg := range args {
		if err := ctx.VFS.Touch(arg); err != nil {
			fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", touch.Name(), arg, err)
			exit = 1
		}
	}
	return exit
}

func init() { Register(Touch{}) }
