package filesystem

import (
	"time"

	"parrot/internal/engine/user"
)

// Info describes one entry: the same fields "ls -l" prints. It is what a
// filesystem hands back in place of whatever it keeps internally.
type Info struct {
	Name    string
	Path    string
	Ino     uint64 // what "ls -i" prints; 0 where there is no such number
	Mode    FileMode
	Owner   user.Identity
	Size    int64
	Links   int
	ModTime time.Time
}

func (i Info) IsDir() bool { return i.Mode.IsDirectory() }
