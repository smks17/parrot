package dev

import "io"

// Device is the stream behind a device file.
type Device interface {
	io.Reader
	io.Writer
}

// discard is the write half of every device that takes bytes and drops them.
func discard(p []byte) (int, error) { return len(p), nil }
