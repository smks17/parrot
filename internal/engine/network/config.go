package network

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"

	"parrot/internal/engine/filesystem"
)

const (
	HostsPath = "/etc/hosts"
	ConfPath  = "/etc/resolv.conf"

	DefaultHosts = "127.0.0.1\tlocalhost\n::1\tlocalhost ip6-localhost ip6-loopback\n"
	DefaultConf  = "nameserver 8.8.8.8\nnameserver 8.8.4.4\n"
)

type Config struct {
	Hosts       []Host       // /etc/hosts
	Nameservers []netip.Addr // /etc/resolv.conf
}

type DB struct {
	fs *filesystem.FS
}

func (db *DB) LoadConfig() (Config, error) {
	var config Config
	// Hostname is optional until /etc/hostname is implemented.
	f, err := (*db.fs).OpenDefault(HostsPath)
	if err != nil {
		return Config{}, fmt.Errorf("open %s: %w", HostsPath, err)
	}
	hosts, err := ReadHosts(f)
	if err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", HostsPath, err)
	}
	config.Hosts = hosts

	f, err = (*db.fs).OpenDefault(ConfPath)
	if err != nil {
		return Config{}, fmt.Errorf("open %s: %w", ConfPath, err)
	}
	defer f.Close()

	config.Nameservers, err = ReadResolveConf(f)
	if err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", ConfPath, err)
	}

	return config, nil
}

func readFile(files filesystem.FS, path string) ([]byte, error) {
	f, err := files.OpenDefault(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(f)
}

func ReadHosts(r io.Reader) ([]Host, error) {
	var hosts []Host
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "#")
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		addr, err := netip.ParseAddr(fields[0])
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, Host{Addr: addr, Names: fields[1:]})
	}
	return hosts, nil
}

func ReadResolveConf(r io.Reader) ([]netip.Addr, error) {
	var nameservers []netip.Addr
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()

		if i := strings.IndexAny(line, "#;"); i >= 0 {
			line = line[:i]
		}

		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "nameserver" {
			continue
		}
		if len(fields) < 2 || net.ParseIP(fields[1]) == nil {
			return nil, fmt.Errorf("invalid nameserver directive: %q", line)
		}
		addr, err := netip.ParseAddr(fields[1])
		if err != nil {
			continue
		}
		nameservers = append(nameservers, addr)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(nameservers) == 0 {
		return nil, errors.New("no nameservers configured")
	}

	return nameservers, nil
}
