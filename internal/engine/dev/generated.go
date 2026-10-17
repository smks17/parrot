package dev

import (
	"bytes"
	"io"

	"parrot/internal/engine/stream"
)

// Generated is a file whose content is made when it is opened, the way sysfs
// and procfs files are: nothing is stored, and each open reads it afresh.
type Generated struct {
	Content func() []byte
}

// Read and Write make Generated a Device, so it can sit on an inode. They are
// only reached if something reads it without going through Open.
func (Generated) Read([]byte) (int, error)  { return 0, io.EOF }
func (Generated) Write([]byte) (int, error) { return 0, stream.ErrBadFD }
func (g Generated) Open() io.ReadWriter     { return readOnly{bytes.NewReader(g.Content())} }

type readOnly struct{ *bytes.Reader }

func (readOnly) MayBlock() bool            { return false }
func (readOnly) Write([]byte) (int, error) { return 0, stream.ErrBadFD }
