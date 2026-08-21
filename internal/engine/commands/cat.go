package commands

import "fmt"

type Cat struct{}

var _ Command = Cat{}

func (cat Cat) Name() string {
	return "cat"
}

func (cat Cat) Usage() string { return "cat file...  — print file contents" }

func (cat Cat) Run(ctx *Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(ctx.Stderr, "%s: missing operand\n", cat.Name())
		return 1
	}

	exit := 0
	for _, arg := range args {
		content, err := ctx.VFS.Read(arg)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", cat.Name(), arg, err)
			exit = 1
			continue
		}
		ctx.Stdout.Write(content)
	}
	return exit
}

func init() { Register(Cat{}) }
