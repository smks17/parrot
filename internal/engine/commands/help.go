package commands

import "fmt"

type Help struct{}

var _ Command = Help{}

func (help Help) Name() string { return "help" }

func (help Help) Usage() string { return "help [command]  — list commands, or show one's usage" }

func (help Help) Run(ctx *Context, args []string) int {
	if len(args) > 0 {
		cmd, exist := Lookup(args[0])
		if !exist {
			fmt.Fprintf(ctx.Stderr, "%s: %s: no such command\n", help.Name(), args[0])
			return 1
		}
		fmt.Fprintln(ctx.Stdout, cmd.Usage())
		return 0
	}

	for _, cmd := range All() {
		if h, ok := cmd.(Hidden); ok && h.Hidden() {
			continue
		}
		fmt.Fprintln(ctx.Stdout, cmd.Usage())
	}
	return 0
}

func init() { Register(Help{}) }
