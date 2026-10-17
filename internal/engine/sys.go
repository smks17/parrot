package engine

import (
	"path"

	"parrot/internal/engine/dev"
	"parrot/internal/engine/network"
	"parrot/internal/engine/vfs"
)

const sysClassNet = "/sys/class/net"

func (app *App) seedSys() {
	app.network.OnNewInterface(app.addSysInterface)
	for _, name := range app.network.Interfaces() {
		app.addSysInterface(name)
	}
}

func (app *App) addSysInterface(name string) {
	tree, inMemory := app.session.tree()
	if !inMemory {
		return // the real filesystem's /sys is the host's own
	}
	addSysInterface(tree, app.network, name)
}

func addSysInterface(tree *vfs.VFS, nm *network.NetworkManager, name string) {
	dir := path.Join(sysClassNet, name)
	if tree.MakeDirAll(dir) != nil {
		return
	}
	operstate := dev.Generated{Content: func() []byte {
		if up, _ := nm.LinkUp(name); up {
			return []byte("up\n")
		}
		return []byte("down\n")
	}}
	_ = tree.MakeDevice(path.Join(dir, "operstate"), operstate, 0444)
}
