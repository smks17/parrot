package vfs

import (
	"path"

	"parrot/internal/engine/filesystem"
	"parrot/internal/engine/user"
)

func seedEtc(root *Inode) {
	files := []struct {
		name, content string
		mode          filesystem.FileMode
	}{
		{path.Base(user.PasswdPath), user.DefaultPasswd, 0644},
		{path.Base(user.GroupPath), user.DefaultGroup, 0644},
		{path.Base(user.ShadowPath), user.DefaultShadow, 0600},
	}

	if !root.IsDir() {
		return
	}

	etcName := path.Base(user.EtcDir)
	etc, ok := root.Lookup(etcName)
	if !ok {
		etc = NewDir(rootOwner())
		_ = root.Link(etcName, etc)
	}
	// Someone has put a file where /etc goes. Leave it alone rather than
	// destroying it to make room.
	if !etc.IsDir() {
		return
	}

	for _, f := range files {
		if _, exists := etc.Lookup(f.name); exists {
			continue
		}
		_ = etc.Link(f.name, NewFile([]byte(f.content), rootOwner()).setOwner(rootOwner(), f.mode))
	}
}
