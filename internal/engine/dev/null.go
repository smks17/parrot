package dev

import "io"

// /dev/null
type Null struct{}

func (Null) MayBlock() bool              { return false }
func (Null) Read(p []byte) (int, error)  { return 0, io.EOF }
func (Null) Write(p []byte) (int, error) { return discard(p) }
