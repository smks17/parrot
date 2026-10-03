package commands

import (
	"fmt"
	"strings"
)

type Groups struct{}

var _ Command = Groups{}

func (c Groups) Name() string { return "groups" }

func (c Groups) Usage() string { return "groups  — print the groups the current user belongs to" }

func (c Groups) Run(ctx *Context, args []string) int {
	groups := ctx.VFS.Identity().Groups
	groupNames := make([]string, len(groups))
	for i, g := range groups {
		groupNames[i] = g.Name
	}
	out := strings.Join(groupNames, " ")
	fmt.Fprintln(ctx.Stdout, out)
	return 0
}

func init() { Register(Groups{}) }
