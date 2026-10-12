package network

import (
	"net/netip"
	"sync"

	"parrot/internal/engine/stream"
)

type Socket struct {
	mu      sync.Mutex
	network Network
	closed  bool

	// What Bind asked for. bound tells "bind to port 0" apart from no Bind.
	bound     bool
	localIP   netip.Addr
	localPort uint16

	listener Listener     // set by Listen
	conn     Conn         // set by Connect or Accept
	file     *stream.File // the description over conn
}

func NewSocket(n Network) *Socket {
	return &Socket{network: n}
}

func connected(n Network, conn Conn) *Socket {
	return &Socket{network: n, conn: conn, file: stream.NewHostFile(conn, stream.O_RDWR)}
}

func (s *Socket) Bind(ip netip.Addr, port uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return stream.ErrBadFD
	case s.bound, s.conn != nil, s.listener != nil:
		return ErrInvalid // a socket has one local address, and it is set once
	}
	s.bound, s.localIP, s.localPort = true, ip, port
	return nil
}

func (s *Socket) Connect(ip netip.Addr, port uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return stream.ErrBadFD
	case s.conn != nil:
		return ErrIsConnected
	case s.listener != nil:
		return ErrInvalid
	}
	conn, err := s.network.Connect(s.localIP, s.localPort, ip, port)
	if err != nil {
		return err
	}
	s.conn, s.file = conn, stream.NewHostFile(conn, stream.O_RDWR)
	return nil
}

func (s *Socket) Listen(backlog int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return stream.ErrBadFD
	case s.conn != nil, s.listener != nil:
		return ErrInvalid
	}
	ip := s.localIP
	if !ip.IsValid() {
		ip = netip.IPv4Unspecified()
	}
	listener, err := s.network.Listen(ip, s.localPort, backlog)
	if err != nil {
		return err
	}
	s.listener = listener
	return nil
}

func (s *Socket) Accept() (*Socket, error) {
	s.mu.Lock()
	listener, closed := s.listener, s.closed
	s.mu.Unlock()
	switch {
	case closed:
		return nil, stream.ErrBadFD
	case listener == nil:
		return nil, ErrInvalid
	}
	conn, err := listener.Accept()
	if err != nil {
		return nil, err
	}
	return connected(s.network, conn), nil
}

func (s *Socket) Shutdown(how ShutdownHow) error {
	s.mu.Lock()
	conn, closed := s.conn, s.closed
	s.mu.Unlock()
	switch {
	case closed:
		return stream.ErrBadFD
	case conn == nil:
		return ErrNotConnected
	}
	switch how {
	case ShutdownRead:
		return conn.CloseRead()
	case ShutdownWrite:
		return conn.CloseWrite()
	case ShutdownBoth:
		if err := conn.CloseRead(); err != nil {
			return err
		}
		return conn.CloseWrite()
	}
	return ErrInvalid
}

type ShutdownHow int

const (
	ShutdownRead ShutdownHow = iota
	ShutdownWrite
	ShutdownBoth
)

func (s *Socket) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return stream.ErrBadFD
	}
	s.closed = true
	switch {
	case s.listener != nil:
		return s.listener.Close()
	case s.conn != nil:
		err := s.conn.Close()
		s.file.Close()
		return err
	}
	return nil // never connected or listening, so there is nothing to hang up
}

func (s *Socket) File() *stream.File {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file
}

func (s *Socket) LocalAddr() (netip.Addr, uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.conn != nil:
		return s.conn.LocalAddr()
	case s.listener != nil:
		return s.listener.Addr()
	}
	return s.localIP, s.localPort
}

func (s *Socket) RemoteAddr() (netip.Addr, uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return netip.Addr{}, 0
	}
	return s.conn.RemoteAddr()
}

func (s *Socket) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		return "socket connected to " + netip.AddrPortFrom(s.conn.RemoteAddr()).String()
	}
	if s.listener != nil {
		return "socket listening on " + netip.AddrPortFrom(s.listener.Addr()).String()
	}
	return "socket unconnected"
}

func (s *Socket) Read(p []byte) (int, error) {
	s.mu.Lock()
	file, closed := s.file, s.closed
	s.mu.Unlock()
	switch {
	case closed:
		return 0, stream.ErrBadFD
	case file == nil:
		return 0, ErrNotConnected
	}
	return file.Read(p)
}

func (s *Socket) Write(p []byte) (int, error) {
	s.mu.Lock()
	file, closed := s.file, s.closed
	s.mu.Unlock()
	switch {
	case closed:
		return 0, stream.ErrBadFD
	case file == nil:
		return 0, ErrNotConnected
	}
	return file.Write(p)
}
