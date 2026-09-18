package commands

import "fmt"

type Su struct{}

var _ Command = Su{}

func (c Su) Name() string { return "su" }

func (c Su) Usage() string { return "su [user]  — switch user (default root)" }

func (c Su) Run(ctx *Context, args []string) int {
	name := "root"
	if len(args) >= 1 {
		name = args[0]
	}
	err := ctx.SetUser(name)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %v\n", c.Name(), err)
		return 1
	}
	return 0
}

func init() { Register(Su{}) }
