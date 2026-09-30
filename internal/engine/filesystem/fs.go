package filesystem

import "parrot/internal/engine/user"

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

	Read(path string) ([]byte, error)
	Write(path string, content []byte, appending bool) error
	Create(path string) error
	Mkdir(path string) error
	Remove(path string, recursive bool) error
	Copy(src, dst string, recursive bool) error
	Move(src, dst string) error
	Touch(path string) error

	Chmod(path string, mode FileMode) error
	Chown(path string, owner user.Identity) error
}
