package stream

import "errors"

// The errors of open files and pipes. filesystem uses the same values, so
// errors.Is matches whichever package a caller names.
var (
	ErrBadFD          = errors.New("bad file descriptor")
	ErrBrokenPipe     = errors.New("broken pipe")
	ErrIsDir          = errors.New("is a directory")
	ErrNotImplemented = errors.New("not implemented")
	ErrNoProcess      = errors.New("no such device or address")
)
