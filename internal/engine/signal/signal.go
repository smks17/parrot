package signal

import (
	"errors"

	"parrot/internal/engine/vfs"
)

var ErrInterrupted = errors.New("interrupted")

func Signalled(err error) (int, bool) {
	switch {
	case errors.Is(err, ErrInterrupted):
		return 130, true
	case errors.Is(err, vfs.ErrBrokenPipe):
		return 141, true
	}
	return 0, false
}
