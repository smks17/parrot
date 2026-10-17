package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"parrot/internal/engine/filesystem"
)

// Resolver is the upstream DNS lookup. *net.Resolver satisfies it.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type resolver struct {
	upstream Resolver
	db       *DB
}

func NewResolver(fs *filesystem.FS) *resolver {
	db := &DB{fs: fs}
	dialer := &net.Dialer{}

	upstream := &net.Resolver{
		PreferGo: true,
		Dial: func(
			ctx context.Context,
			network string,
			_ string,
		) (net.Conn, error) {
			config, err := db.LoadConfig()
			if err != nil {
				return nil, fmt.Errorf("load DNS config: %w", err)
			}

			if len(config.Nameservers) == 0 {
				return nil, errors.New("no nameservers configured")
			}

			server := net.JoinHostPort(
				config.Nameservers[0].String(),
				"53",
			)

			return dialer.DialContext(ctx, network, server)
		},
	}

	return &resolver{upstream: upstream, db: db}
}

// Resolve returns the addresses for hostname: an IP literal as is,
// then /etc/hosts-style entries, then the upstream DNS server.
func (r *resolver) Resolve(
	ctx context.Context,
	hostname string,
) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(hostname); err == nil {
		return []netip.Addr{addr}, nil
	}

	addrs, err := r.lookupHosts(hostname)
	if err != nil {
		return nil, err
	}
	if len(addrs) > 0 {
		return addrs, nil
	}

	return r.upstream.LookupNetIP(ctx, "ip", hostname)
}

type Host struct {
	Addr  netip.Addr
	Names []string
}

func (r *resolver) lookupHosts(hostname string) ([]netip.Addr, error) {
	hostname = strings.TrimSuffix(hostname, ".")
	var addrs []netip.Addr
	config, err := r.db.LoadConfig()
	if err != nil {
		return nil, err
	}
	for _, host := range config.Hosts {
		for _, name := range host.Names {
			if strings.EqualFold(name, hostname) {
				addrs = append(addrs, host.Addr)
				break
			}
		}
	}
	return addrs, nil
}
