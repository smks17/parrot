package commands

import (
	"fmt"
	"strings"
)

type Ls struct{}

var _ Command = Ls{}

func (ls Ls) Name() string {
	return "ls"
}

func (ls Ls) Usage() string { return "ls [-a] [path]  — list directory contents" }

func (ls Ls) Run(ctx *Context, args []string) int {
	var all bool
	var operands []string
	for _, arg := range args {
		if arg == "-a" {
			all = true
			continue
		}
		operands = append(operands, arg)
	}

	path := ctx.VFS.Cwd()
	if len(operands) > 0 {
		path = operands[0]
	}

	nodes, err := ctx.VFS.List(path)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", ls.Name(), path, err)
		return 1
	}

	for _, node := range nodes {
		if !all && strings.HasPrefix(node.Name, ".") {
			continue
		}
		name := node.Name
		if node.IsDir {
			name += "/"
		}
		fmt.Fprintln(ctx.Stdout, name)
	}
	return 0
}

func init() { Register(Ls{}) }
