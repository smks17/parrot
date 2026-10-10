package vfs

import (
	"parrot/internal/engine/dev"
	"parrot/internal/engine/filesystem"
)

const devDir = "dev"

func seedDev(root *Inode) {
	devices := []struct {
		name   string
		device dev.Device
	}{
		{"null", dev.Null{}},
		{"zero", dev.Zero{}},
		{"random", dev.Random{}},
		{"urandom", dev.Random{}},
		{"stdin", dev.Stdin()},
		{"stdout", dev.Stdout()},
		{"stderr", dev.Stderr()},
	}

	if !root.IsDir() {
		return
	}
	dir, ok := root.Lookup(devDir)
	if !ok {
		dir = NewDir(rootOwner())
		_ = root.Link(devDir, dir)
	}
	// Someone has put a file where /dev goes. Leave it alone, as seedEtc does.
	if !dir.IsDir() {
		return
	}

	for _, d := range devices {
		if _, exists := dir.Lookup(d.name); exists {
			continue
		}
		// 0666, as on Linux: anyone may read and write these, and only root
		// may remove them, because /dev is root's 0755.
		_ = dir.Link(d.name, NewDevice(d.device, rootOwner(), filesystem.FileMode(0666)))
	}
}
