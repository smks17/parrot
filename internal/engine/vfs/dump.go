package vfs

import (
	"time"

	"parrot/internal/engine/filesystem"
)

type DumpNode struct {
	Name     string      `json:"name"`
	Content  []byte      `json:"content,omitempty"`
	Children []*DumpNode `json:"children,omitempty"`

	Mode  *filesystem.FileMode `json:"mode,omitempty"`
	Owner string               `json:"owner,omitempty"`
	Group string               `json:"group,omitempty"`

	// Mtime is Unix seconds.
	Mtime int64 `json:"mtime,omitempty"`

	Ino uint64 `json:"ino,omitempty"`
}

func (node *Inode) Dump(name string) *DumpNode {
	return node.dump(name, map[Ino]bool{})
}

func (node *Inode) dump(name string, written map[Ino]bool) *DumpNode {
	d := &DumpNode{Name: name, Ino: uint64(node.ino)}
	if written[node.ino] {
		return d
	}
	written[node.ino] = true

	owner, mode := node.meta()
	d.Mode, d.Owner, d.Group = &mode, owner.User, owner.Group
	d.Content = node.Bytes()
	// A zero Time means "unknown"
	if mtime := node.ModTime(); !mtime.IsZero() {
		d.Mtime = mtime.Unix()
	}
	for _, entry := range node.Entries() {
		d.Children = append(d.Children, entry.Inode.dump(entry.Name, written))
	}
	return d
}

func (d *DumpNode) ToInode() *Inode {
	return d.toInode(map[uint64]*Inode{}, true)
}

func (d *DumpNode) toInode(byIno map[uint64]*Inode, isRoot bool) *Inode {
	if d.Ino != 0 {
		if linked, ok := byIno[d.Ino]; ok {
			return linked // a second name for an inode already rebuilt
		}
		reserveIno(Ino(d.Ino))
	}

	owner := Ownership{User: d.Owner, Group: d.Group}

	mode, known := filesystem.DefaultFileMode, false
	if d.Mode != nil {
		mode, known = *d.Mode, true
	}
	isDir := mode.IsDirectory() || (!known && len(d.Children) > 0)

	var n *Inode
	switch {
	case isRoot:
		n = newRoot(owner)
	case isDir:
		n = NewDir(owner)
	default:
		n = NewFile(d.Content, owner)
	}
	if known {
		n.setOwner(owner, mode)
	}
	if d.Ino != 0 {
		byIno[d.Ino] = n
	}

	for _, child := range d.Children {
		_ = n.Link(child.Name, child.toInode(byIno, false))
	}

	if d.Mtime != 0 {
		n.lock(func() { n.mtime = time.Unix(d.Mtime, 0) })
	}
	return n
}
