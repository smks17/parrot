package commands

import "fmt"

type Whoami struct{}

var _ Command = Whoami{}

func (c Whoami) Name() string { return "whoami" }

func (c Whoami) Usage() string { return "whoami  — print the current user name" }

func (c Whoami) Run(ctx *Context, args []string) int {
	fmt.Fprintln(ctx.Stdout, ctx.VFS.User())
	return 0
}

func init() { Register(Whoami{}) }
