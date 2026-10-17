package network

import (
	"net/netip"
	"sync"

	"parrot/internal/engine/stream"
)

// The ports Linux hands out when a socket does not choose one
// (/proc/sys/net/ipv4/ip_local_port_range).
const (
	firstEphemeralPort = 32768
	lastEphemeralPort  = 60999
)

// LO is the loopback interface: a network that never leaves Parrot, where
// one process connects to another listening in the same machine. It is pure
// Go, so unlike the host's network it works in the browser too.
type LO struct {
	mu        sync.Mutex
	listeners map[netip.AddrPort]*loListener
	nextPort  uint16
}

var _ Network = (*LO)(nil)

func NewLO() *LO {
	return &LO{listeners: map[netip.AddrPort]*loListener{}, nextPort: firstEphemeralPort}
}

func (lo *LO) Connect(localIP netip.Addr, localPort uint16, ip netip.Addr, port uint16) (Conn, error) {
	if !ip.IsLoopback() {
		return nil, ErrConnRefused
	}
	if localIP.IsValid() && !localIP.IsLoopback() {
		return nil, ErrInvalid
	}

	lo.mu.Lock()
	defer lo.mu.Unlock()
	listener := lo.listenerAt(ip, port)
	if listener == nil {
		return nil, ErrConnRefused
	}
	if !localIP.IsValid() {
		localIP = ip
	}
	if localPort == 0 {
		localPort = lo.ephemeralPort()
	}

	toServer, toClient := stream.NewBuffer(), stream.NewBuffer()
	client := &loConn{in: toClient, out: toServer,
		localIP: localIP, localPort: localPort, remoteIP: ip, remotePort: port}
	server := &loConn{in: toServer, out: toClient,
		localIP: ip, localPort: port, remoteIP: localIP, remotePort: localPort}

	select {
	case listener.queue <- server:
		return client, nil
	default:
		client.Close()
		return nil, ErrConnRefused
	}
}

func (lo *LO) Listen(ip netip.Addr, port uint16, backlog int) (Listener, error) {
	if !ip.IsLoopback() && !ip.IsUnspecified() {
		return nil, ErrInvalid
	}
	lo.mu.Lock()
	defer lo.mu.Unlock()
	if port == 0 {
		port = lo.ephemeralPort()
	}
	addr := netip.AddrPortFrom(ip, port)
	if lo.listeners[addr] != nil {
		return nil, ErrAddrInUse
	}
	listener := &loListener{
		lo:    lo,
		addr:  addr,
		queue: make(chan *loConn, max(backlog, 1)),
		done:  make(chan struct{}),
	}
	lo.listeners[addr] = listener
	return listener, nil
}

func (lo *LO) Close() error {
	lo.mu.Lock()
	listeners := make([]*loListener, 0, len(lo.listeners))
	for _, listener := range lo.listeners {
		listeners = append(listeners, listener)
	}
	lo.mu.Unlock()
	for _, listener := range listeners {
		listener.Close()
	}
	return nil
}

func (lo *LO) listenerAt(ip netip.Addr, port uint16) *loListener {
	if listener := lo.listeners[netip.AddrPortFrom(ip, port)]; listener != nil {
		return listener
	}
	unspecified := netip.IPv4Unspecified()
	if ip.Is6() {
		unspecified = netip.IPv6Unspecified()
	}
	return lo.listeners[netip.AddrPortFrom(unspecified, port)]
}

// ephemeralPort picks the next port in the range, wrapping round at its end.
func (lo *LO) ephemeralPort() uint16 {
	port := lo.nextPort
	lo.nextPort++
	if lo.nextPort > lastEphemeralPort {
		lo.nextPort = firstEphemeralPort
	}
	return port
}

// loConn is one end of a loopback connection
type loConn struct {
	in, out *stream.Buffer

	localIP, remoteIP     netip.Addr
	localPort, remotePort uint16
}

func (c *loConn) Read(p []byte) (int, error)  { return c.in.Read(p) }
func (c *loConn) Write(p []byte) (int, error) { return c.out.Write(p) }
func (c *loConn) MayBlock() bool              { return true }
func (c *loConn) CloseWrite() error           { c.out.Close(); return nil }
func (c *loConn) CloseRead() error            { c.in.Close(); return nil }

func (c *loConn) Close() error {
	c.out.Close()
	c.in.Close()
	return nil
}

func (c *loConn) LocalAddr() (netip.Addr, uint16)  { return c.localIP, c.localPort }
func (c *loConn) RemoteAddr() (netip.Addr, uint16) { return c.remoteIP, c.remotePort }

// loListener is a listening address on lo: the queue of connections that
// Connect made and Accept has not yet taken.
type loListener struct {
	lo    *LO
	addr  netip.AddrPort
	queue chan *loConn
	done  chan struct{} // closed by Close, to wake Accept

	closeOnce sync.Once
}

func (l *loListener) Accept() (Conn, error) {
	select {
	case conn := <-l.queue:
		return conn, nil
	case <-l.done:
		return nil, stream.ErrBadFD
	}
}

func (l *loListener) Close() error {
	err := stream.ErrBadFD
	l.closeOnce.Do(func() {
		l.lo.mu.Lock()
		delete(l.lo.listeners, l.addr)
		l.lo.mu.Unlock()
		close(l.done)
		for {
			select {
			case conn := <-l.queue:
				conn.Close()
			default:
				err = nil
				return
			}
		}
	})
	return err
}

func (l *loListener) Addr() (netip.Addr, uint16) { return l.addr.Addr(), l.addr.Port() }
