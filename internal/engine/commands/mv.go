package commands

import (
	"fmt"
)

type Mv struct{}

var _ Command = Mv{}

func (mv Mv) Name() string { return "mv" }

func (mv Mv) Usage() string { return "mv src dst  — move or rename a file" }

func (mv Mv) Run(ctx *Context, args []string) int {
	if len(args) < 2 {
		fmt.Fprintf(ctx.Stderr, "%s: missing file operand\n", mv.Name())
		return 1
	}
	srcPath := args[0]
	dstPath := args[1]

	if err := ctx.VFS.Move(srcPath, dstPath); err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %s %s: %v\n", mv.Name(), dstPath, srcPath, err)
		return 1
	}
	return 0
}
func init() { Register(Mv{}) }
