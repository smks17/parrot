package commands

import "fmt"

type Pwd struct{}

var _ Command = Pwd{}

func (pwd Pwd) Name() string {
	return "pwd"
}

func (pwd Pwd) Usage() string { return "pwd  — print the current working directory" }

func (pwd Pwd) Run(ctx *Context, args []string) int {
	fmt.Fprintln(ctx.Stdout, ctx.VFS.Cwd())
	return 0
}

func init() { Register(Pwd{}) }
