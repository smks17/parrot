package vfs

// FileMode holds the nine permission bits as a Unix-style octal value.
// 0644 == rw-r--r--, 0755 == rwxr-xr-x.
//
// The directory flag lives above the nine permission bits, at 040000 —
// the same place Unix puts S_IFDIR. Keeping it out of the low 0777 lets
// chmod mask with 0777 and never accidentally strip the type.
type FileMode uint16

const (
	// Owner permissions
	ModeOwnerRead    FileMode = 0400
	ModeOwnerWrite   FileMode = 0200
	ModeOwnerExecute FileMode = 0100

	// Group permissions
	ModeGroupRead    FileMode = 0040
	ModeGroupWrite   FileMode = 0020
	ModeGroupExecute FileMode = 0010

	// Other permissions
	ModeOtherRead    FileMode = 0004
	ModeOtherWrite   FileMode = 0002
	ModeOtherExecute FileMode = 0001

	// File type
	ModeDirectory FileMode = 1 << 14
)

const (
	DefaultFileMode FileMode = 0644
	DefaultDirMode  FileMode = 0755 | ModeDirectory
)

type PermClass int8

const (
	OtherUserPerm PermClass = iota
	UserPerm
	GroupPerm
)

func (m FileMode) String() string {
	var b [10]byte
	if m&ModeDirectory != 0 {
		b[0] = 'd'
	} else {
		b[0] = '-'
	}
	const chars = "rwx"
	for i := 0; i < 9; i++ {
		if m&(FileMode(1)<<(8-i)) != 0 {
			b[i+1] = chars[i%3]
		} else {
			b[i+1] = '-'
		}
	}
	return string(b[:])
}

func (m FileMode) CanRead(permClass PermClass) bool {
	switch permClass {
	case GroupPerm:
		return m&ModeGroupRead != 0
	case UserPerm:
		return m&ModeOwnerRead != 0
	default:
		return m&ModeOtherRead != 0
	}
}

func (m FileMode) CanWrite(permClass PermClass) bool {
	switch permClass {
	case GroupPerm:
		return m&ModeGroupWrite != 0
	case UserPerm:
		return m&ModeOwnerWrite != 0
	default:
		return m&ModeOtherWrite != 0
	}
}

func (m FileMode) CanExecute(permClass PermClass) bool {
	switch permClass {
	case GroupPerm:
		return m&ModeGroupExecute != 0
	case UserPerm:
		return m&ModeOwnerExecute != 0
	default:
		return m&ModeOtherExecute != 0
	}
}

func (m FileMode) IsDirectory() bool {
	return m&ModeDirectory != 0
}

// permBits is what a VFS operation needs from a node. Create and Remove
// need write AND exec on the parent directory — never on the entry.
type permBits uint8

const (
	permRead permBits = 1 << iota
	permWrite
	permExec
)

func (f *VFS) getPermClass(n *Node) PermClass {
	if n.Owner == f.id.Name {
		return UserPerm
	}
	if f.id.InGroup(n.Group) {
		return GroupPerm
	}
	return OtherUserPerm
}

// checkPerm is the gate in front of every VFS operation. It compares the
// VFS's current user against the node's owner, picks the matching bit
// class, and refuses with ErrPermission when a needed bit is missing.
func (f *VFS) checkPerm(n *Node, need permBits) error {
	if n == nil {
		return ErrNotExist
	}
	if f.id.IsRoot() {
		return nil
	}

	permClass := f.getPermClass(n)
	if need&permRead != 0 && !n.Mode.CanRead(permClass) {
		return ErrPermission
	}
	if need&permWrite != 0 && !n.Mode.CanWrite(permClass) {
		return ErrPermission
	}
	if need&permExec != 0 && !n.Mode.CanExecute(permClass) {
		return ErrPermission
	}
	return nil
}

// checkOwnership gates the metadata-changing operations (chmod, chown):
// only the file's owner may change them. That is Unix EPERM — a different
// error from the EACCES that checkPerm returns.
func (f *VFS) checkOwnership(n *Node) error {
	if n == nil {
		return ErrNotExist
	}
	if f.id.IsRoot() {
		return nil
	}
	if n.Owner != "" && n.Owner != f.id.Name {
		return ErrNotOwner
	}
	return nil
}
