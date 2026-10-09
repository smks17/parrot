package filesystem

import (
	"errors"

	"parrot/internal/engine/stream"
)

var (
	ErrNotExist    = errors.New("no such file or directory")
	ErrNotDir      = errors.New("not a directory")
	ErrExists      = errors.New("file exists")
	ErrNotEmptyDir = errors.New("directory not empty")
	ErrRootRemove  = errors.New("cannot remove root directory")
	ErrPermission  = errors.New("permission denied")
	ErrNotOwner    = errors.New("operation not permitted")
	ErrInvalid     = errors.New("invalid argument")
	ErrLinkDir     = errors.New("hard link not allowed for directory")

	// These belong to open files, which live in stream; they are the same
	// values here so a caller can name either package.
	ErrIsDir          = stream.ErrIsDir
	ErrBadFD          = stream.ErrBadFD
	ErrNotImplemented = stream.ErrNotImplemented
	ErrBrokenPipe     = stream.ErrBrokenPipe
)
