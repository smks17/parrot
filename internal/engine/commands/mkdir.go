package commands

import "fmt"

type Mkdir struct{}

var _ Command = Mkdir{}

func (mkdir Mkdir) Name() string {
	return "mkdir"
}

func (mkdir Mkdir) Usage() string { return "mkdir dir...  — create directories" }

func (mkdir Mkdir) Run(ctx *Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(ctx.Stderr, "%s: missing operand\n", mkdir.Name())
		return 1
	}

	exit := 0
	for _, arg := range args {
		if err := ctx.VFS.Mkdir(arg); err != nil {
			fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", mkdir.Name(), arg, err)
			exit = 1
		}
	}
	return exit
}

func init() { Register(Mkdir{}) }
