package vfs

import "errors"

var (
	ErrNotExist    = errors.New("no such file or directory")
	ErrNotDir      = errors.New("not a directory")
	ErrIsDir       = errors.New("is a directory")
	ErrExists      = errors.New("file exists")
	ErrNotEmptyDir = errors.New("directory not empty")
	ErrRootRemove  = errors.New("cannot remove root directory")
	ErrPermission  = errors.New("permission denied")
	ErrNotOwner    = errors.New("operation not permitted")
)
