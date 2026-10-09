package filesystem

import (
	"parrot/internal/engine/stream"
	"parrot/internal/engine/user"
)

// FS is everything a command needs from a filesystem.
type FS interface {
	Cwd() string
	Chdir(path string) error

	Identity() user.Identity
	User() string
	Group() string

	Stat(path string) (Info, error)
	List(path string) ([]Info, error)
	Walk(path string, do func(info Info) error) error

	Open(path string, flags stream.OpenFlags) (*stream.File, error)
	OpenDefault(path string) (*stream.File, error)

	Read(path string) ([]byte, error)
	Write(path string, content []byte, appending bool) error
	Create(path string) error
	Mkdir(path string) error
	Remove(path string, recursive bool) error
	Copy(src, dst string, recursive bool) error
	Move(src, dst string) error
	Link(oldpath, newpath string) error
	Touch(path string) error

	Chmod(path string, mode FileMode) error
	// Chown takes names, not an Identity: an empty one means "leave this
	// alone", which is how "chown :staff f" changes only the group.
	Chown(path string, owner, group string) error
}
