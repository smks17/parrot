package commands

import "fmt"

const clearScreen = "\033[2J\033[H"

type Clear struct{}

var _ Command = Clear{}

func (clear Clear) Name() string {
	return "clear"
}

func (clear Clear) Usage() string { return "clear  — clear the screen" }

func (clear Clear) Run(ctx *Context, args []string) int {
	fmt.Fprintln(ctx.Stdout, clearScreen)
	return 0
}

func init() { Register(Clear{}) }
