//go:build unix

package main

import (
	"os"
	"syscall"
)

// suspendSignals is what Ctrl+Z sends: the terminal's SIGTSTP. Caught only
// while a command runs, so at the prompt Ctrl+Z still stops prt itself.
var suspendSignals = []os.Signal{syscall.SIGTSTP}
