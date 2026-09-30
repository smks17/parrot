package filesystem

import "parrot/internal/engine/user"

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
type PermBits uint8

const (
	PermRead PermBits = 1 << iota
	PermWrite
	PermExec
)

func PermClassOf(owner, actor user.Identity) PermClass {
	if owner.Name == actor.Name {
		return UserPerm
	}
	if actor.InGroup(owner.Primary()) {
		return GroupPerm
	}
	return OtherUserPerm
}

func CheckPerm(mode FileMode, owner, actor user.Identity, need PermBits) error {
	if actor.IsRoot() {
		return nil
	}

	class := PermClassOf(owner, actor)
	if need&PermRead != 0 && !mode.CanRead(class) {
		return ErrPermission
	}
	if need&PermWrite != 0 && !mode.CanWrite(class) {
		return ErrPermission
	}
	if need&PermExec != 0 && !mode.CanExecute(class) {
		return ErrPermission
	}
	return nil
}

func CheckOwnership(owner, actor user.Identity) error {
	if actor.IsRoot() {
		return nil
	}
	if owner.Name != actor.Name {
		return ErrNotOwner
	}
	return nil
}
