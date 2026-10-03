package vfs

import "parrot/internal/engine/filesystem"

// checkPerm is the gate in front of every VFS operation.
func (f *VFS) checkPerm(n *Inode, need filesystem.PermBits) error {
	if n == nil {
		return filesystem.ErrNotExist
	}
	owner, mode := n.meta()
	return filesystem.CheckPerm(mode, owner, f.id, need)
}

// checkOwnership gates the metadata-changing operations, chmod and chown.
func (f *VFS) checkOwnership(n *Inode) error {
	if n == nil {
		return filesystem.ErrNotExist
	}
	return filesystem.CheckOwnership(n.Owner(), f.id)
}
