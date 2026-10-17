package hostnet

import (
	"errors"
	"io"
	"net"
	"net/netip"
	"syscall"

	"parrot/internal/engine/network"
)

// Net is the host's TCP network.
type Net struct{}

var _ network.Network = Net{}

func (Net) Connect(localIP netip.Addr, localPort uint16, ip netip.Addr, port uint16) (network.Conn, error) {
	var local *net.TCPAddr
	if localIP.IsValid() || localPort != 0 {
		local = net.TCPAddrFromAddrPort(netip.AddrPortFrom(localIP, localPort))
	}
	c, err := net.DialTCP("tcp", local, net.TCPAddrFromAddrPort(netip.AddrPortFrom(ip, port)))
	if err != nil {
		return nil, translate(err)
	}
	return conn{c}, nil
}

// Listen ignores backlog: Go chooses the host's own and offers no way to set
// it.
func (Net) Listen(ip netip.Addr, port uint16, backlog int) (network.Listener, error) {
	l, err := net.ListenTCP("tcp", net.TCPAddrFromAddrPort(netip.AddrPortFrom(ip, port)))
	if err != nil {
		return nil, translate(err)
	}
	return listener{l}, nil
}

// conn gives a TCP connection the addresses the engine speaks, and the error
// a broken pipe has everywhere else in it.
type conn struct {
	*net.TCPConn
}

func (c conn) Write(p []byte) (int, error) {
	n, err := c.TCPConn.Write(p)
	if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
		err = io.ErrClosedPipe
	}
	return n, err
}

// MayBlock is true: a read waits on the peer for as long as it likes
func (c conn) MayBlock() bool { return true }

func (c conn) LocalAddr() (netip.Addr, uint16)  { return split(c.TCPConn.LocalAddr()) }
func (c conn) RemoteAddr() (netip.Addr, uint16) { return split(c.TCPConn.RemoteAddr()) }

type listener struct {
	*net.TCPListener
}

func (l listener) Accept() (network.Conn, error) {
	c, err := l.AcceptTCP()
	if err != nil {
		return nil, translate(err)
	}
	return conn{c}, nil
}

func (l listener) Addr() (netip.Addr, uint16) { return split(l.TCPListener.Addr()) }

func split(addr net.Addr) (netip.Addr, uint16) {
	ap := addr.(*net.TCPAddr).AddrPort()
	return ap.Addr().Unmap(), ap.Port() // an IPv4 peer, not ::ffff:a.b.c.d
}

// translate turns a host error into the one the engine already names, so a
// command can test for it with errors.Is whichever network it went through.
func translate(err error) error {
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return network.ErrConnRefused
	case errors.Is(err, syscall.EADDRINUSE):
		return network.ErrAddrInUse
	}
	return err
}

// Addr is the host's address on the network its default route leads to
func Addr() (netip.Prefix, error) {
	probe, err := net.Dial("udp", "192.0.2.1:9") // TEST-NET-1: never answered, always routed by default
	if err != nil {
		return netip.Prefix{}, err
	}
	local := probe.LocalAddr().(*net.UDPAddr).AddrPort().Addr().Unmap()
	probe.Close()

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return netip.Prefix{}, err
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip, ok := netip.AddrFromSlice(ipnet.IP)
		if !ok || ip.Unmap() != local {
			continue
		}
		bits, _ := ipnet.Mask.Size()
		return netip.PrefixFrom(local, bits), nil
	}
	return netip.PrefixFrom(local, local.BitLen()), nil // the host never said; take the address alone
}
