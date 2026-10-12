package network

import (
	"errors"
	"io"
	"net/netip"
)

// Network is where a socket's addresses lead.
type Network interface {
	Connect(localIP netip.Addr, localPort uint16, ip netip.Addr, port uint16) (Conn, error)
	Listen(ip netip.Addr, port uint16, backlog int) (Listener, error)
}

// Conn is a connection a network made.
type Conn interface {
	io.ReadWriteCloser
	CloseRead() error
	CloseWrite() error
	LocalAddr() (netip.Addr, uint16)
	RemoteAddr() (netip.Addr, uint16)
}

// Listener is a listening socket's queue of peers that have connected and
// not yet been accepted. Closing it wakes an Accept that is waiting.
type Listener interface {
	Accept() (Conn, error)
	Close() error
	Addr() (netip.Addr, uint16)
}

var (
	ErrConnRefused  = errors.New("connection refused")
	ErrIsConnected  = errors.New("transport endpoint is already connected")
	ErrNotConnected = errors.New("transport endpoint is not connected")
	ErrAddrInUse    = errors.New("address already in use")
	ErrInvalid      = errors.New("invalid argument")

	ErrNetUnreachable = errors.New("network is unreachable")
	ErrNetDown        = errors.New("network is down")
	ErrNotPermitted   = errors.New("Operation not permitted")
	ErrNoDevice       = errors.New("no such device")
)
