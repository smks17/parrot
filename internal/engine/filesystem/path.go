package filesystem

import (
	"path"
	"strings"
)

func Split(p string) (dir, name string) {
	trimmed := strings.TrimRight(p, "/")
	if trimmed == "" {
		return "/", ""
	}

	i := strings.LastIndex(trimmed, "/")
	if i < 0 {
		return ".", trimmed
	}
	if dir = trimmed[:i]; dir == "" {
		dir = "/"
	}
	return dir, trimmed[i+1:]
}

// NameFor is the name a copy or a move gives its result when the destination
// is a directory: the source's last element, with trailing slashes ignored so
// that "cp dir/ elsewhere" lands on "elsewhere/dir" rather than on nothing.
func NameFor(src string) string {
	return path.Base(strings.TrimRight(src, "/"))
}
