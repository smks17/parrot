package user

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const (
	EtcDir     = "/etc"
	PasswdPath = "/etc/passwd"
	GroupPath  = "/etc/group"

	RootName  = "root"
	RootGroup = "root"
	RootUID   = 0

	DevGroup = "dev"
)

const (
	passwdFields = 6 // name:x:uid:gid:gecos:home
	groupFields  = 4 // name:x:gid:member,member
)

const (
	DefaultPasswd = "root:x:0:0:root:/root\n" +
		"mahdi:x:1000:1000:mahdi:/home/mahdi\n"

	DefaultGroup = "root:x:0:\n" +
		"users:x:1000:\n" +
		"dev:x:100:mahdi\n"
)

func ErrNoUser(name string) error {
	return fmt.Errorf("invalid user: %s", name)
}

func ErrNoGroup(name string) error {
	return fmt.Errorf("invalid group: %s", name)
}

type Group struct {
	Name string
	GID  int
}

type Identity struct {
	Name   string
	UID    int
	GID    int
	Groups []Group // primary group first, then supplementary
}

func (i Identity) IsRoot() bool { return i.Name != "" && i.UID == RootUID }

func (i Identity) Primary() string {
	if len(i.Groups) == 0 {
		return ""
	}
	return i.Groups[0].Name
}

func (i Identity) InGroup(name string) bool {
	return slices.ContainsFunc(i.Groups, func(g Group) bool { return g.Name == name })
}

// TODO: Later remove this and use fs interface
type Reader interface {
	ReadRaw(path string) ([]byte, error)
}

type DB struct {
	r Reader
}

func NewDB(r Reader) *DB { return &DB{r: r} }

func (d *DB) read(path string) string {
	b, err := d.r.ReadRaw(path)
	if err != nil {
		return ""
	}
	return string(b)
}

type passwdEntry struct {
	Name string
	UID  int
	GID  int
	Home string
}

type groupEntry struct {
	Name    string
	GID     int
	Members []string
}

func fields(line string, want int) ([]string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil, false
	}
	f := strings.Split(line, ":")
	if len(f) != want {
		return nil, false
	}
	return f, true
}

func parsePasswd(content string) []passwdEntry {
	var entries []passwdEntry
	for line := range strings.Lines(content) {
		f, ok := fields(line, passwdFields)
		if !ok {
			continue
		}
		uid, err := strconv.Atoi(f[2])
		if err != nil {
			continue
		}
		gid, err := strconv.Atoi(f[3])
		if err != nil {
			continue
		}
		entries = append(entries, passwdEntry{Name: f[0], UID: uid, GID: gid, Home: f[5]})
	}
	return entries
}

func parseGroup(content string) []groupEntry {
	var entries []groupEntry
	for line := range strings.Lines(content) {
		f, ok := fields(line, groupFields)
		if !ok {
			continue
		}
		gid, err := strconv.Atoi(f[2])
		if err != nil {
			continue
		}
		// A trailing colon means no members, not one empty-named member.
		var members []string
		if f[3] != "" {
			members = strings.Split(f[3], ",")
		}
		entries = append(entries, groupEntry{Name: f[0], GID: gid, Members: members})
	}
	return entries
}

// Lookup resolves a username into a full Identity:
//   - the /etc/passwd entry supplies UID and GID
//   - the primary group is the /etc/group line whose GID matches that GID
//   - supplementary groups are every line listing name among its members
//
// Returns ErrNoUser when the name is not in /etc/passwd.
func (d *DB) Lookup(name string) (Identity, error) {
	entries := parsePasswd(d.read(PasswdPath))
	i := slices.IndexFunc(entries, func(p passwdEntry) bool { return p.Name == name })
	if i < 0 {
		return Identity{}, ErrNoUser(name)
	}
	p := entries[i]

	id := Identity{Name: p.Name, UID: p.UID, GID: p.GID}

	// Two buckets, so the primary lands first however the file is ordered.
	// The GID match wins over the member list, which keeps a user who is
	// also listed in their own primary group from appearing in it twice.
	var supplementary []Group
	for _, g := range parseGroup(d.read(GroupPath)) {
		switch {
		case g.GID == p.GID:
			id.Groups = append(id.Groups, Group{Name: g.Name, GID: g.GID})
		case slices.Contains(g.Members, name):
			supplementary = append(supplementary, Group{Name: g.Name, GID: g.GID})
		}
	}
	id.Groups = append(id.Groups, supplementary...)

	// A GID matching no group line still yields a usable account, just one
	// with no primary group name. A missing group is not a missing user.
	return id, nil
}

func (d *DB) UserExists(name string) bool {
	_, err := d.Lookup(name)
	return err == nil
}

func (d *DB) GroupExists(name string) bool {
	return slices.ContainsFunc(parseGroup(d.read(GroupPath)),
		func(g groupEntry) bool { return g.Name == name })
}
