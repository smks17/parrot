//go:build !unix

package osfs

import (
	"errors"
	"os"
	osuser "os/user"

	"parrot/internal/engine/user"
)

func owner(os.FileInfo) user.Identity { return user.Identity{} }

func current() user.Identity {
	found, err := osuser.Current()
	if err != nil {
		return user.Identity{}
	}
	return user.Identity{Name: found.Username, Home: found.HomeDir}
}

func linkCount(os.FileInfo) int { return 1 }

func ids(string, string) (int, int, error) {
	return 0, 0, errors.New("changing the owner is not supported on this system")
}
