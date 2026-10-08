//go:build !unix

package main

import "os"

// suspendSignals is empty where the console has no Ctrl+Z to catch.
var suspendSignals []os.Signal
