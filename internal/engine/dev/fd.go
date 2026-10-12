package dev

import "parrot/internal/engine/stream"

type Descriptor struct {
	fd int
}

// Read and Write make Descriptor a Device, so it can sit on an inode. They
// are only reached if something opens it without resolving its Target.
func (Descriptor) Read([]byte) (int, error)  { return 0, stream.ErrNoProcess }
func (Descriptor) Write([]byte) (int, error) { return 0, stream.ErrNoProcess }

// Target is the description the opener's fd points at.
func (d Descriptor) Target(fds *stream.FDTable) (*stream.File, error) {
	if fds == nil {
		return nil, stream.ErrNoProcess
	}
	return fds.File(d.fd)
}
