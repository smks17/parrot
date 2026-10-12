package network

import (
	"context"
	"io"
	"net/netip"
	"sync"
)

type NetworkManager struct {
	LO *LO

	mu         sync.RWMutex
	interfaces map[string]Network
	routes     []route    // longest prefix first, so the first match is the one to use
	published  []Listener // the outside listeners of published ports
	down       bool

	ctx      context.Context
	shutdown chan struct{}
	stopOnce sync.Once
}

var _ Network = (*NetworkManager)(nil)

// route is one line of "ip route": the addresses in prefix go through the
// interface called device.
type route struct {
	prefix netip.Prefix
	device string
}

func NewNetworkManager(ctx context.Context) *NetworkManager {
	lo := NewLO() // one loopback, under both names, or a listener on one is missing from the other
	return &NetworkManager{
		LO: lo,
		interfaces: map[string]Network{
			"lo": lo,
		},
		routes: []route{
			{netip.MustParsePrefix("::1/128"), "lo"},
			{netip.MustParsePrefix("127.0.0.0/8"), "lo"},
		},
		ctx:      ctx,
		shutdown: make(chan struct{}),
	}
}

func (nm *NetworkManager) GetInterface(name string) (Network, bool) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	iface, ok := nm.interfaces[name]
	return iface, ok
}

func (nm *NetworkManager) AddInterface(name string, network Network) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.interfaces[name] = network
}

func (nm *NetworkManager) AddRoute(prefix netip.Prefix, device string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	if _, ok := nm.interfaces[device]; !ok {
		return ErrNoDevice
	}
	prefix = prefix.Masked()
	at := len(nm.routes)
	for i, r := range nm.routes {
		if prefix.Bits() > r.prefix.Bits() {
			at = i
			break
		}
	}
	nm.routes = append(nm.routes[:at], append([]route{{prefix, device}}, nm.routes[at:]...)...)
	return nil
}

func (nm *NetworkManager) SetDefaultRoute(device string) error {
	if err := nm.AddRoute(netip.MustParsePrefix("0.0.0.0/0"), device); err != nil {
		return err
	}
	return nm.AddRoute(netip.MustParsePrefix("::/0"), device)
}

// routeTo finds the interface ip is reached through.
func (nm *NetworkManager) routeTo(ip netip.Addr) (Network, error) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	if nm.down {
		return nil, ErrNetDown
	}
	ip = ip.Unmap()
	for _, r := range nm.routes {
		if r.prefix.Contains(ip) {
			return nm.interfaces[r.device], nil
		}
	}
	return nil, ErrNetUnreachable
}

func (nm *NetworkManager) Connect(localIP netip.Addr, localPort uint16, ip netip.Addr, port uint16) (Conn, error) {
	iface, err := nm.routeTo(ip)
	if err != nil {
		return nil, err
	}
	return iface.Connect(localIP, localPort, ip, port)
}

// TODO: listen on every bridge the machine is attached to as well.
func (nm *NetworkManager) Listen(ip netip.Addr, port uint16, backlog int) (Listener, error) {
	if ip.IsUnspecified() {
		nm.mu.RLock()
		down := nm.down
		nm.mu.RUnlock()
		if down {
			return nil, ErrNetDown
		}
		return nm.LO.Listen(ip, port, backlog)
	}
	iface, err := nm.routeTo(ip)
	if err != nil {
		return nil, err
	}
	return iface.Listen(ip, port, backlog)
}

// Publish is peers that connects then in outside are forwarded to port on this machine's lo.
func (nm *NetworkManager) Publish(outside Network, outsideIP netip.Addr, outsidePort, port uint16) error {
	listener, err := outside.Listen(outsideIP, outsidePort, 128)
	if err != nil {
		return err
	}
	nm.mu.Lock()
	nm.published = append(nm.published, listener)
	nm.mu.Unlock()

	// Ends when takeDown closes the listener.
	go func() {
		for {
			peer, err := listener.Accept()
			if err != nil {
				return
			}
			go nm.forward(peer, port)
		}
	}()
	return nil
}

// forward outside peer to port inside
func (nm *NetworkManager) forward(peer Conn, port uint16) {
	defer peer.Close()
	target, err := nm.LO.Connect(netip.Addr{}, 0, netip.MustParseAddr("127.0.0.1"), port)
	if err != nil {
		return // nobody listening inside: the peer is hung up on
	}
	defer target.Close()
	go func() {
		io.Copy(target, peer)
		target.CloseWrite()
	}()
	io.Copy(peer, target)
}

// TODO: Later use systemd
func (nm *NetworkManager) Start() {
	go func() {
		select {
		case <-nm.shutdown:
		case <-nm.ctx.Done():
		}
		nm.takeDown()
	}()
}

func (nm *NetworkManager) Stop() {
	nm.stopOnce.Do(func() { close(nm.shutdown) })
}

// takeDown refuses every Connect and Listen from now on, then closes every interface
// when an interface goes down.
func (nm *NetworkManager) takeDown() {
	nm.mu.Lock()
	nm.down = true
	interfaces := make([]Network, 0, len(nm.interfaces))
	for _, iface := range nm.interfaces {
		interfaces = append(interfaces, iface)
	}
	published := nm.published
	nm.published = nil
	nm.mu.Unlock()
	for _, listener := range published {
		listener.Close()
	}
	for _, iface := range interfaces {
		if closer, ok := iface.(io.Closer); ok {
			closer.Close()
		}
	}
}
