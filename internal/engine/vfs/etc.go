package vfs

import (
	"path"
	"strings"

	"parrot/internal/engine/user"
)

func seedEtc(root *Node) {
	files := []struct{ name, content string }{
		{path.Base(user.PasswdPath), user.DefaultPasswd},
		{path.Base(user.GroupPath), user.DefaultGroup},
	}

	if !root.IsDir() {
		return
	}

	etcName := path.Base(user.EtcDir)
	etc, ok := root.Children[etcName]
	if !ok {
		etc = NewDir(etcName).setOwner(user.RootName, user.RootGroup, 0755|ModeDirectory)
		root.AddChild(etc)
	}
	// Someone has put a file where /etc goes. Leave it alone rather than
	// destroying it to make room.
	if !etc.IsDir() {
		return
	}

	for _, f := range files {
		if _, exists := etc.Children[f.name]; exists {
			continue
		}
		etc.AddChild(NewFile(f.name, []byte(f.content)).
			setOwner(user.RootName, user.RootGroup, 0644))
	}
}

func (f *VFS) ReadRaw(p string) ([]byte, error) {
	curr := f.root
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
