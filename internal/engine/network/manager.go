package network

import (
	"context"
	"errors"
	"io"
	"net/netip"
	"slices"
	"sync"
)

type NetworkManager struct {
	LO *LO

	mu         sync.RWMutex
	interfaces map[string]Network
	order      []string                  // the names in the order they were added; an interface's index is its place here, from 1
	up         map[string]bool           // ip link set NAME up|down; a down link carries nothing
	addrs      map[string][]netip.Prefix // ip addr: each address with the length of its subnet, as 192.168.1.5/24
	routes     []Route                   // longest prefix first, so the first match is the one to use
	published  []Listener                // the outside listeners of published ports
	down       bool

	// onNewInterface is told the name of every interface added, the way the
	// kernel's netdevice notifier tells sysfs to make its directory.
	onNewInterface func(name string)

	ctx      context.Context
	shutdown chan struct{}
	stopOnce sync.Once
}

var _ Network = (*NetworkManager)(nil)

// Route is one line of "ip route": the addresses in Prefix go through the
// interface called Device.
type Route struct {
	Prefix netip.Prefix
	Device string
}

func NewNetworkManager(ctx context.Context) *NetworkManager {
	lo := NewLO() // one loopback, under both names, or a listener on one is missing from the other
	nm := &NetworkManager{
		LO: lo,
		interfaces: map[string]Network{
			"lo": lo,
		},
		order:    []string{"lo"},
		up:       map[string]bool{"lo": true},
		addrs:    map[string][]netip.Prefix{},
		ctx:      ctx,
		shutdown: make(chan struct{}),
	}
	// lo's routes come with its addresses, as every interface's do.
	nm.addAddress("lo", netip.MustParsePrefix("127.0.0.1/8"))
	nm.addAddress("lo", netip.MustParsePrefix("::1/128"))
	return nm
}

func (nm *NetworkManager) GetInterface(name string) (Network, bool) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	iface, ok := nm.interfaces[name]
	return iface, ok
}

func (nm *NetworkManager) AddInterface(name string, network Network) {
	nm.mu.Lock()
	if _, replaced := nm.interfaces[name]; !replaced {
		nm.order = append(nm.order, name)
	}
	nm.interfaces[name] = network
	nm.up[name] = true
	notify := nm.onNewInterface
	nm.mu.Unlock()
	// Outside the lock, so the hook may ask the manager about the interface.
	if notify != nil {
		notify(name)
	}
}

// OnNewInterface installs the hook told about every interface added from now on.
func (nm *NetworkManager) OnNewInterface(notify func(name string)) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.onNewInterface = notify
}

// Interfaces names every interface in the order it was added, lo first.
func (nm *NetworkManager) Interfaces() []string {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return slices.Clone(nm.order)
}

// AddAddress is ip addr add ADDR dev DEVICE. Like Linux it also routes the
// address's subnet through the device, so 192.168.1.5/24 on eth0 brings
// 192.168.1.0/24 dev eth0 with it.
func (nm *NetworkManager) AddAddress(device string, addr netip.Prefix) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	return nm.addAddress(device, addr)
}

func (nm *NetworkManager) addAddress(device string, addr netip.Prefix) error {
	if _, ok := nm.interfaces[device]; !ok {
		return ErrNoDevice
	}
	if !addr.IsValid() {
		return ErrInvalid
	}
	if slices.Contains(nm.addrs[device], addr) {
		return ErrExists
	}
	nm.addrs[device] = append(nm.addrs[device], addr)
	if err := nm.addRoute(addr.Masked(), device); err != nil && !errors.Is(err, ErrExists) {
		return err
	}
	return nil
}

// Addresses are the addresses on device, in the order they were added.
func (nm *NetworkManager) Addresses(device string) ([]netip.Prefix, error) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	if _, ok := nm.interfaces[device]; !ok {
		return nil, ErrNoDevice
	}
	return slices.Clone(nm.addrs[device]), nil
}

// Routes is the routing table, longest prefix first.
func (nm *NetworkManager) Routes() []Route {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return slices.Clone(nm.routes)
}

func (nm *NetworkManager) SetLinkUp(name string, up bool) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	if _, ok := nm.interfaces[name]; !ok {
		return ErrNoDevice
	}
	nm.up[name] = up
	return nil
}

func (nm *NetworkManager) LinkUp(name string) (bool, error) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	if _, ok := nm.interfaces[name]; !ok {
		return false, ErrNoDevice
	}
	return nm.up[name], nil
}

func (nm *NetworkManager) AddRoute(prefix netip.Prefix, device string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	return nm.addRoute(prefix, device)
}

func (nm *NetworkManager) addRoute(prefix netip.Prefix, device string) error {
	if _, ok := nm.interfaces[device]; !ok {
		return ErrNoDevice
	}
	prefix = prefix.Masked()
	if slices.Contains(nm.routes, Route{prefix, device}) {
		return ErrExists // ip route add refuses a route it already has
	}
	at := len(nm.routes)
	for i, r := range nm.routes {
		if prefix.Bits() > r.Prefix.Bits() {
			at = i
			break
		}
	}
	nm.routes = slices.Insert(nm.routes, at, Route{prefix, device})
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
		if r.Prefix.Contains(ip) && nm.up[r.Device] {
			return nm.interfaces[r.Device], nil
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
	target, err := nm.Connect(netip.Addr{}, 0, netip.MustParseAddr("127.0.0.1"), port)
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
