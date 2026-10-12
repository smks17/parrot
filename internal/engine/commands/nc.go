package commands

import (
	"fmt"
	"io"
	"net/netip"
	"strconv"

	"parrot/internal/engine/network"
	"parrot/internal/engine/proc"
	"parrot/internal/engine/signal"
)

type Nc struct{}

var _ Command = Nc{}

func (nc Nc) Name() string { return "nc" }

func (nc Nc) Usage() string {
	return "nc [-N] HOST PORT | nc -l [-N] PORT  — connect to a port, or listen on one"
}

func (nc Nc) Run(ctx *Context, args []string) int {
	listen, shutdownOnEOF := false, false
	var operands []string
	for _, arg := range args {
		switch arg {
		case "-l":
			listen = true
		case "-N":
			shutdownOnEOF = true
		default:
			operands = append(operands, arg)
		}
	}
	if (listen && len(operands) != 1) || (!listen && len(operands) != 2) {
		fmt.Fprintf(ctx.Stderr, "usage: %s\n", nc.Usage())
		return 1
	}
	if ctx.Network == nil {
		fmt.Fprintf(ctx.Stderr, "%s: %v\n", nc.Name(), network.ErrNetUnreachable)
		return 1
	}

	port, err := strconv.ParseUint(operands[len(operands)-1], 10, 16)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: port number invalid: %s\n", nc.Name(), operands[len(operands)-1])
		return 1
	}

	var conn *network.Socket
	if listen {
		conn, err = nc.accept(ctx, uint16(port))
	} else {
		conn, err = nc.connect(ctx, operands[0], uint16(port))
	}
	if ctx.Interrupted() {
		return 130
	}
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %v\n", nc.Name(), err)
		return 1
	}
	return nc.relay(ctx, conn, shutdownOnEOF)
}

func (nc Nc) connect(ctx *Context, host string, port uint16) (*network.Socket, error) {
	ip, err := netip.ParseAddr(host)
	if host == "localhost" {
		ip, err = netip.MustParseAddr("127.0.0.1"), nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: invalid address", host)
	}
	sock := network.NewSocket(ctx.Network)
	if err := sock.Connect(ip, port); err != nil {
		sock.Close()
		return nil, fmt.Errorf("connect to %s port %d: %w", ip, port, err)
	}
	return sock, nil
}

func (nc Nc) accept(ctx *Context, port uint16) (*network.Socket, error) {
	listener := network.NewSocket(ctx.Network)
	defer listener.Close() // one peer is all nc takes
	listener.Bind(netip.IPv4Unspecified(), port)
	if err := listener.Listen(1); err != nil {
		return nil, fmt.Errorf("port %d: %w", port, err)
	}
	stop := closeOnInterrupt(ctx, listener)
	defer stop()
	return listener.Accept()
}

// relay copies both ways until the peer hangs up or the user presses Ctrl-C.
func (nc Nc) relay(ctx *Context, conn *network.Socket, shutdownOnEOF bool) int {
	stop := closeOnInterrupt(ctx, conn)
	defer stop()
	defer conn.Close()

	if ctx.Stdin != nil {
		// Ends at the end of stdin, or when the connection is closed under it.
		go func() {
			io.Copy(conn, ctx.Stdin)
			if shutdownOnEOF {
				conn.Shutdown(network.ShutdownWrite)
			}
		}()
	}

	// Through the guard, so a wait on the peer gives up the CPU the way a
	// wait on a pipe does.
	in := proc.StreamGuard{Context: ctx.Ctx, Process: ctx.Self}.WrapReader(conn.File())
	_, err := io.Copy(ctx.Stdout, in)
	if ctx.Interrupted() {
		return 130
	}
	if status, signalled := signal.Signalled(err); signalled {
		return status
	}
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %v\n", nc.Name(), err)
		return 1
	}
	return 0
}

func closeOnInterrupt(ctx *Context, sock *network.Socket) (stop func()) {
	if ctx.Ctx == nil || ctx.Ctx.Done() == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Ctx.Done():
			sock.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

func init() { Register(Nc{}) }
