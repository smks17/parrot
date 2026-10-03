package osfs

import (
	"errors"
	"os"

	"parrot/internal/engine/filesystem"
)

// translate turns an operating system error into the one the commands
// already print, so their messages read the same in both worlds.
func translate(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrNotExist):
		return filesystem.ErrNotExist
	case errors.Is(err, os.ErrExist):
		return filesystem.ErrExists
	case errors.Is(err, os.ErrPermission):
		return filesystem.ErrPermission
	}

	// The rest have no counterpart in the in-memory tree; unwrap the path
	// noise that os puts around them and keep the message.
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		switch pathErr.Err.Error() {
		case "not a directory":
			return filesystem.ErrNotDir
		case "is a directory":
			return filesystem.ErrIsDir
		case "directory not empty":
			return filesystem.ErrNotEmptyDir
		}
		return pathErr.Err
	}
	return err
}
