package vfs

import (
	"path"

	"parrot/internal/engine/user"
)

func seedEtc(root *Node, rootUser *user.Identity) {
	files := []struct {
		name, content string
		mode          FileMode
	}{
		{path.Base(user.PasswdPath), user.DefaultPasswd, 0644},
		{path.Base(user.GroupPath), user.DefaultGroup, 0644},
		{path.Base(user.ShadowPath), user.DefaultShadow, 0600},
	}

	if !root.IsDir() {
		return
	}

	etcName := path.Base(user.EtcDir)
	etc, ok := root.Children[etcName]
	if !ok {
		etc = NewDir(etcName, *rootUser)
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
		etc.AddChild(NewFile(f.name, []byte(f.content), *rootUser))
	}
}
