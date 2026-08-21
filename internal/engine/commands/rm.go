package commands

import (
	"fmt"
)

type Rm struct{}

var _ Command = Rm{}

func (rm Rm) Name() string { return "rm" }

func (rm Rm) Usage() string { return "rm [-r] path  — remove a file or directory" }

func (rm Rm) Run(ctx *Context, args []string) int {
	var receives bool
	var operands []string
	for _, arg := range args {
		if arg == "-r" {
			receives = true
			continue
		}
		operands = append(operands, arg)
	}

	path := operands[0]

	if err := ctx.VFS.Remove(path, receives); err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", rm.Name(), path, err)
		return 1
	}
	return 0
}
func init() { Register(Rm{}) }
