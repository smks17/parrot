package commands

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"parrot/internal/engine/network"
)

type IP struct{}

var _ Command = IP{}

func (IP) Name() string { return "ip" }
func (IP) Usage() string {
	return "ip addr [show [dev] IFACE | add|del ADDR[/LEN] dev IFACE] | ip link [show [IFACE] | set [dev] IFACE up|down] | ip [-4|-6] route" +
		"  — show and change interfaces, their addresses and the routing table"
}

// actions are the verbs ip knows, in the order an abbreviation is matched
// against them, so s is show and not set.
var actions = []string{"show", "list", "add", "delete", "set"}

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

	// ip takes any prefix of an object's or an action's name, so a, ad and
	// addr are all address, and with no action it shows.
	object, rest := args[0], args[1:]
	action := "show"
	if len(rest) > 0 && rest[0] != "" {
		for _, a := range actions {
			if strings.HasPrefix(a, rest[0]) {
				action, rest = a, rest[1:]
				break
			}
		}
	}
	if action == "list" {
		action = "show"
	}

	switch {
	case strings.HasPrefix("address", object) && action == "show":
		return c.addresses(ctx, rest)
	case strings.HasPrefix("address", object) && (action == "add" || action == "delete"):
		return c.changeAddress(ctx, action == "add", rest)
	case strings.HasPrefix("link", object) && action == "show":
		return c.links(ctx, rest)
	case strings.HasPrefix("link", object) && action == "set":
		return c.setLink(ctx, rest)
	case strings.HasPrefix("route", object) && action == "show" && len(rest) == 0:
		return c.routes(ctx, family)
	case strings.HasPrefix("address", object), strings.HasPrefix("link", object), strings.HasPrefix("route", object):
		fmt.Fprintf(ctx.Stderr, "%s: %s %s is not supported\nusage: %s\n", c.Name(), object, strings.Join(rest, " "), c.Usage())
		return 1
	}
	fmt.Fprintf(ctx.Stderr, "%s: object %q is unknown\n", c.Name(), object)
	return 1
}

// changeAddress is ip addr add|del ADDR[/LEN] dev IFACE. An address with no
// length is a single host's, /32 or /128, as ip takes it.
func (c IP) changeAddress(ctx *Context, add bool, args []string) int {
	var device, given string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "dev" && i+1 < len(args):
			device = args[i+1]
			i++
		case given == "":
			given = args[i]
		default:
			given = "" // two addresses: refuse below rather than guess
			i = len(args)
		}
	}
	if device == "" || given == "" {
		fmt.Fprintf(ctx.Stderr, "usage: %s\n", c.Usage())
		return 1
	}
	addr, err := parseInterfaceAddr(given)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "Error: inet prefix is expected rather than %q.\n", given)
		return 1
	}
	if !c.mayChange(ctx) {
		return 2
	}
	if add {
		err = ctx.Network.AddAddress(device, addr)
	} else {
		err = ctx.Network.DelAddress(device, addr)
	}
	return c.report(ctx, device, err)
}

// links prints what ip addr does without the addresses.
func (c IP) links(ctx *Context, args []string) int {
	if len(args) > 0 && args[0] == "dev" {
		args = args[1:]
	}
	names := ctx.Network.Interfaces()
	if len(args) == 1 {
		if _, err := ctx.Network.LinkUp(args[0]); err != nil {
			return c.report(ctx, args[0], err)
		}
		names = []string{args[0]}
	} else if len(args) > 1 {
		fmt.Fprintf(ctx.Stderr, "usage: %s\n", c.Usage())
		return 1
	}
	for _, name := range names {
		c.linkLine(ctx, name)
	}
	return 0
}

// setLink is ip link set [dev] IFACE up|down. Unlike ifup it says nothing
// when the link already is the way it was asked to be.
func (c IP) setLink(ctx *Context, args []string) int {
	if len(args) > 0 && args[0] == "dev" {
		args = args[1:]
	}
	if len(args) != 2 || (args[1] != "up" && args[1] != "down") {
		fmt.Fprintf(ctx.Stderr, "usage: %s\n", c.Usage())
		return 1
	}
	if !c.mayChange(ctx) {
		return 2
	}
	return c.report(ctx, args[0], ctx.Network.SetLinkUp(args[0], args[1] == "up"))
}

// mayChange is the kernel's check that only root reconfigures the network.
func (c IP) mayChange(ctx *Context) bool {
	if ctx.User.IsRoot() {
		return true
	}
	fmt.Fprintf(ctx.Stderr, "RTNETLINK answers: %v\n", network.ErrNotPermitted)
	return false
}

// report prints an error the way ip does: a missing device by name, and
// anything the kernel refused as RTNETLINK's answer, with ip's exit statuses:
// 1 for a device it could not find, 2 for a refusal.
func (c IP) report(ctx *Context, device string, err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, network.ErrNoDevice):
		fmt.Fprintf(ctx.Stderr, "Cannot find device %q\n", device)
		return 1
	default:
		fmt.Fprintf(ctx.Stderr, "RTNETLINK answers: %v\n", err)
		return 2
	}
}

// parseInterfaceAddr reads 192.168.1.5/24, or 192.168.1.5 as /32.
func parseInterfaceAddr(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	ip, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(ip, ip.BitLen()), nil
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
			return c.report(ctx, args[0], err)
		}
		names = []string{args[0]}
	} else if len(args) > 1 {
		fmt.Fprintf(ctx.Stderr, "usage: %s\n", c.Usage())
		return 1
	}

	for _, name := range names {
		c.linkLine(ctx, name)
		addrs, _ := ctx.Network.Addresses(name)
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

// linkLine is an interface's first line in ip addr and ip link: index, name,
// flags and state.
func (c IP) linkLine(ctx *Context, name string) {
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
	fmt.Fprintf(ctx.Stdout, "%d: %s: <%s> state %s\n", index(ctx.Network.Interfaces(), name), name, strings.Join(flags, ","), state)
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
