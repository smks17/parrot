//go:build unix

package osfs

import (
	"fmt"
	"os"
	osuser "os/user"
	"strconv"
	"sync"
	"syscall"

	"parrot/internal/engine/user"
)

// current asks the system who is running the shell, and what groups they are
// in. A machine that will not say leaves an empty identity, which reports no
// name rather than the wrong one.
func current() user.Identity {
	found, err := osuser.Current()
	if err != nil {
		return user.Identity{}
	}

	id := user.Identity{Name: found.Username, Home: found.HomeDir}
	id.UID, _ = strconv.Atoi(found.Uid)
	id.GID, _ = strconv.Atoi(found.Gid)

	// The primary group goes first, as it does in the in-memory world: the
	// permission checks there read Groups[0] as the group a new file gets.
	if primary, err := osuser.LookupGroupId(found.Gid); err == nil {
		id.Groups = append(id.Groups, user.Group{Name: primary.Name, GID: id.GID})
	}
	ids, err := found.GroupIds()
	if err != nil {
		return id
	}
	for _, gid := range ids {
		if gid == found.Gid {
			continue // already there, as the primary one
		}
		group, err := osuser.LookupGroupId(gid)
		if err != nil {
			continue
		}
		number, _ := strconv.Atoi(gid)
		id.Groups = append(id.Groups, user.Group{Name: group.Name, GID: number})
	}
	return id
}

var (
	namesMu sync.Mutex
	users   = map[uint32]string{}
	groups  = map[uint32]string{}
)

func owner(stat os.FileInfo) user.Identity {
	sys, ok := stat.Sys().(*syscall.Stat_t)
	if !ok {
		return user.Identity{}
	}

	namesMu.Lock()
	defer namesMu.Unlock()

	name, ok := users[sys.Uid]
	if !ok {
		name = fmt.Sprint(sys.Uid)
		if found, err := osuser.LookupId(name); err == nil {
			name = found.Username
		}
		users[sys.Uid] = name
	}

	group, ok := groups[sys.Gid]
	if !ok {
		group = fmt.Sprint(sys.Gid)
		if found, err := osuser.LookupGroupId(group); err == nil {
			group = found.Name
		}
		groups[sys.Gid] = group
	}

	id := user.Identity{Name: name, UID: int(sys.Uid), GID: int(sys.Gid)}
	if group != "" {
		id.Groups = []user.Group{{Name: group, GID: int(sys.Gid)}}
	}
	return id
}

// linkCount is what "ls -l" shows in its second column.
func linkCount(stat os.FileInfo) int {
	if sys, ok := stat.Sys().(*syscall.Stat_t); ok {
		return int(sys.Nlink)
	}
	return 1
}

// ids turns an owner and group name into the numbers chown needs. An empty
// name means "leave this one alone", which chown spells as -1.
func ids(owner, group string) (int, int, error) {
	uid, gid := -1, -1
	if owner != "" {
		found, err := osuser.Lookup(owner)
		if err != nil {
			return 0, 0, user.ErrNoUser(owner)
		}
		fmt.Sscan(found.Uid, &uid)
	}
	if group != "" {
		found, err := osuser.LookupGroup(group)
		if err != nil {
			return 0, 0, user.ErrNoGroup(group)
		}
		fmt.Sscan(found.Gid, &gid)
	}
	return uid, gid, nil
}
