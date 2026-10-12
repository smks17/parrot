package commands

import (
	"fmt"

	"parrot/internal/engine/network"
)

type Ifup struct{}

var _ Command = Ifup{}

func (Ifup) Name() string  { return "ifup" }
func (Ifup) Usage() string { return "ifup IFACE  — bring a network interface up" }

func (c Ifup) Run(ctx *Context, args []string) int {
	return setLink(ctx, c, args, true)
}

func setLink(ctx *Context, cmd Command, args []string, up bool) int {
	if len(args) != 1 {
		fmt.Fprintf(ctx.Stderr, "usage: %s\n", cmd.Usage())
		return 1
	}
	name := args[0]
	if ctx.Network == nil {
		fmt.Fprintf(ctx.Stderr, "%s: %v\n", cmd.Name(), network.ErrNetUnreachable)
		return 1
	}
	if !ctx.User.IsRoot() {
		fmt.Fprintf(ctx.Stderr, "%s: %v\n", cmd.Name(), network.ErrNotPermitted)
		return 1
	}
	already, err := ctx.Network.LinkUp(name)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", cmd.Name(), name, err)
		return 1
	}
	if already == up {
		state := "already configured"
		if !up {
			state = "not configured"
		}
		fmt.Fprintf(ctx.Stderr, "%s: interface %s %s\n", cmd.Name(), name, state)
		return 0
	}
	if err := ctx.Network.SetLinkUp(name, up); err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", cmd.Name(), name, err)
		return 1
	}
	return 0
}

func init() { Register(Ifup{}) }
