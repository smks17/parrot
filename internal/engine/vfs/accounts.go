package vfs

import (
	"strings"

	"parrot/internal/engine/user"
)

// accounts is how the user database reads /etc/passwd, /etc/group and
// /etc/shadow: straight off the nodes, with no permission check.
//
// The bypass has to exist — authentication runs before there is an identity
// to check against — so it is kept as narrow as it can be. The type is
// unexported and never handed out, which leaves this file as the only way
// into the tree that skips the checks, and /etc/shadow root-only to every
// other one.
type accounts struct{ root *Node }

var _ user.Reader = accounts{}

func (a accounts) ReadRaw(p string) ([]byte, error) {
	curr := a.root
	for seg := range strings.SplitSeq(p, "/") {
		if seg == "" || seg == "." {
			continue
		}
		if !curr.IsDir() {
			return nil, ErrNotDir
		}
		next, ok := curr.Children[seg]
		if !ok {
			return nil, ErrNotExist
		}
		curr = next
	}
	if curr.IsDir() {
		return nil, ErrIsDir
	}
	return curr.Content, nil
}
