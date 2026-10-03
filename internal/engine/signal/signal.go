package signal

import (
	"errors"

	"parrot/internal/engine/filesystem"
)

var ErrInterrupted = errors.New("interrupted")

func Signalled(err error) (int, bool) {
	switch {
	case errors.Is(err, ErrInterrupted):
		return 130, true
	case errors.Is(err, filesystem.ErrBrokenPipe):
		return 141, true
	}
	return 0, false
}
