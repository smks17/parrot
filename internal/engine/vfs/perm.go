package vfs

import "parrot/internal/engine/filesystem"

// checkPerm is the gate in front of every VFS operation.
func (f *VFS) checkPerm(n *Node, need filesystem.PermBits) error {
	if n == nil {
		return filesystem.ErrNotExist
	}
	return filesystem.CheckPerm(n.Mode, n.Owner, f.id, need)
}

// checkOwnership gates the metadata-changing operations, chmod and chown.
func (f *VFS) checkOwnership(n *Node) error {
	if n == nil {
		return filesystem.ErrNotExist
	}
	return filesystem.CheckOwnership(n.Owner, f.id)
}
