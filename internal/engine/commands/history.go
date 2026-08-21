package commands

import "fmt"

type History struct{}

var _ Command = History{}

func (history History) Name() string {
	return "history"
}

func (history History) Usage() string { return "history  — print the history of commands" }

func (history History) Run(ctx *Context, args []string) int {
	for i, line := range ctx.History {
		fmt.Fprintf(ctx.Stdout, "%d  %s\n", i+1, line)
	}
	return 0
}

func init() { Register(History{}) }
