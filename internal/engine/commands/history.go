package commands

import "fmt"

type History struct{}

var _ Command = History{}

func (history History) Name() string {
	return "history"
}

func (history History) Usage() string {
	return "history [-t]  — print the history of commands, optionally with timestamps"
}

const historyTimeFormat = "2006-01-02 15:04:05"

func (history History) Run(ctx *Context, args []string) int {
	var times bool
	for _, arg := range args {
		switch arg {
		case "-t":
			times = true
		default:
			fmt.Fprintf(ctx.Stderr, "%s: invalid option -- '%s'\n", history.Name(), arg)
			return 1
		}
	}
	for i, entry := range ctx.History {
		if !times {
			fmt.Fprintf(ctx.Stdout, "%d  %s\n", i+1, entry.Line)
			continue
		}
		stamp := "-"
		if !entry.At.IsZero() {
			stamp = entry.At.Format(historyTimeFormat)
		}
		fmt.Fprintf(ctx.Stdout, "%d  %s  %s\n", i+1, stamp, entry.Line)
	}
	return 0
}

func init() { Register(History{}) }
