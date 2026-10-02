package vfs

import "errors"

var (
	ErrNotExist       = errors.New("no such file or directory")
	ErrNotDir         = errors.New("not a directory")
	ErrIsDir          = errors.New("is a directory")
	ErrExists         = errors.New("file exists")
	ErrNotEmptyDir    = errors.New("directory not empty")
	ErrRootRemove     = errors.New("cannot remove root directory")
	ErrPermission     = errors.New("permission denied")
	ErrNotOwner       = errors.New("operation not permitted")
	ErrInvalid        = errors.New("invalid argument")
	ErrLinkDir        = errors.New("hard link not allowed for directory")
	ErrBadFD          = errors.New("bad file descriptor")
	ErrNotImplemented = errors.New("not implemented")
	ErrBrokenPipe     = errors.New("broken pipe")
)
