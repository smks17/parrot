package commands

import (
	"fmt"
	"strings"
)

type Chown struct{}

var _ Command = Chown{}

func (c Chown) Name() string { return "chown" }

func (c Chown) Usage() string { return "chown <user>[:<group>] <path>  — change file ownership" }

func (c Chown) Run(ctx *Context, args []string) int {
	if len(args) < 2 {
		fmt.Fprintf(ctx.Stderr, "%s: missing operand\n", c.Name())
		return 1
	}
	spec, path := args[0], args[1]

	owner, group, ok := parseOwnerSpec(spec)
	if !ok {
		fmt.Fprintf(ctx.Stderr, "%s: invalid user spec: '%s'\n", c.Name(), spec)
		return 1
	}

	if err := ctx.VFS.Chown(path, owner, group); err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", c.Name(), path, err)
		return 1
	}
	return 0
}

func parseOwnerSpec(spec string) (owner, group string, ok bool) {
	owner, group, hasColon := strings.Cut(spec, ":")
	if !hasColon {
		return owner, "", owner != ""
	}
	if owner == "" && group == "" {
		return "", "", false // a bare ":" names nobody
	}
	return owner, group, true
}

func init() { Register(Chown{}) }
