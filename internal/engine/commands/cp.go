package commands

import (
	"fmt"
)

type Cp struct{}

var _ Command = Cp{}

func (cp Cp) Name() string { return "cp" }

func (cp Cp) Usage() string { return "cp [-r] src dst  — copy a file or directory" }

func (cp Cp) Run(ctx *Context, args []string) int {
	var receives bool
	var operands []string
	for _, arg := range args {
		if arg == "-r" {
			receives = true
			continue
		}
		operands = append(operands, arg)
	}

	srcPath := operands[0]
	dstPath := operands[1]

	if err := ctx.VFS.Copy(srcPath, dstPath, receives); err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %s %s: %v\n", cp.Name(), dstPath, srcPath, err)
		return 1
	}
	return 0
}
func init() { Register(Cp{}) }
