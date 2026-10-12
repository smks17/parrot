package commands

import (
	"fmt"
	"net/netip"
	"strings"

	"parrot/internal/engine/network"
)

type IP struct{}

var _ Command = IP{}

func (IP) Name() string { return "ip" }
func (IP) Usage() string {
	return "ip addr [show [dev] IFACE] | ip [-4|-6] route  — show interfaces, their addresses and the routing table"
}

func (c IP) Run(ctx *Context, args []string) int {
	family := 4
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "-4":
			family = 4
		case "-6":
			family = 6
		default:
			fmt.Fprintf(ctx.Stderr, "%s: invalid option -- '%s'\nusage: %s\n", c.Name(), args[0], c.Usage())
			return 1
		}
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintf(ctx.Stderr, "usage: %s\n", c.Usage())
		return 1
	}
	if ctx.Network == nil {
		fmt.Fprintf(ctx.Stderr, "%s: %v\n", c.Name(), network.ErrNetUnreachable)
		return 1
	}

	// ip takes any prefix of an object's name, so a, ad and addr are all address.
	object, rest := args[0], args[1:]
	if len(rest) > 0 && rest[0] != "" && (strings.HasPrefix("show", rest[0]) || strings.HasPrefix("list", rest[0])) {
		rest = rest[1:]
	}
	switch {
	case strings.HasPrefix("address", object):
		return c.addresses(ctx, rest)
	case strings.HasPrefix("route", object):
		if len(rest) > 0 {
			fmt.Fprintf(ctx.Stderr, "%s: route: only show is supported\n", c.Name())
			return 1
		}
		return c.routes(ctx, family)
	}
	fmt.Fprintf(ctx.Stderr, "%s: object %q is unknown\n", c.Name(), object)
	return 1
}

// addresses prints each interface the way ip addr does: index, name, flags,
// then one line per address.
func (c IP) addresses(ctx *Context, args []string) int {
	if len(args) > 0 && args[0] == "dev" {
		args = args[1:]
	}
	names := ctx.Network.Interfaces()
	if len(args) == 1 {
		if _, err := ctx.Network.LinkUp(args[0]); err != nil {
			fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", c.Name(), args[0], err)
			return 1
		}
		names = []string{args[0]}
	} else if len(args) > 1 {
		fmt.Fprintf(ctx.Stderr, "usage: %s\n", c.Usage())
		return 1
	}

	all := ctx.Network.Interfaces()
	for _, name := range names {
		up, _ := ctx.Network.LinkUp(name)
		addrs, _ := ctx.Network.Addresses(name)

		var flags []string
		if isLoopback(addrs) {
			flags = append(flags, "LOOPBACK")
		}
		state := "DOWN"
		if up {
			flags = append(flags, "UP")
			state = "UP"
		}
		fmt.Fprintf(ctx.Stdout, "%d: %s: <%s> state %s\n", index(all, name), name, strings.Join(flags, ","), state)
		for _, addr := range addrs {
			kind, label := "inet", " "+name
			if addr.Addr().Is6() {
				kind, label = "inet6", "" // ip labels only IPv4 addresses
			}
			fmt.Fprintf(ctx.Stdout, "    %s %s scope %s%s\n", kind, addr, scope(addr.Addr()), label)
		}
	}
	return 0
}

// routes prints one family's routes, the default first as ip route does. A
// route that came with an address says so, and which address it leaves from.
func (c IP) routes(ctx *Context, family int) int {
	var defaults, others []string
	for _, r := range ctx.Network.Routes() {
		if (family == 6) != r.Prefix.Addr().Is6() {
			continue
		}
		if r.Prefix.Bits() == 0 {
			defaults = append(defaults, fmt.Sprintf("default dev %s", r.Device))
			continue
		}
		line := fmt.Sprintf("%s dev %s", r.Prefix, r.Device)
		addrs, _ := ctx.Network.Addresses(r.Device)
		for _, addr := range addrs {
			if addr.Masked() == r.Prefix {
				line += fmt.Sprintf(" proto kernel scope %s src %s", routeScope(addr.Addr()), addr.Addr())
				break
			}
		}
		others = append(others, line)
	}
	for _, line := range append(defaults, others...) {
		fmt.Fprintln(ctx.Stdout, line)
	}
	return 0
}

// index is the interface's ifindex: its place in the order it was added, from 1.
func index(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i + 1
		}
	}
	return 0
}

func isLoopback(addrs []netip.Prefix) bool {
	for _, addr := range addrs {
		if addr.Addr().IsLoopback() {
			return true
		}
	}
	return false
}

func scope(ip netip.Addr) string {
	if ip.IsLoopback() {
		return "host"
	}
	return "global"
}

func routeScope(ip netip.Addr) string {
	if ip.IsLoopback() {
		return "host"
	}
	return "link" // the subnet is right on the wire: no gateway between
}

func init() { Register(IP{}) }
