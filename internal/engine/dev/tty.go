package dev

import (
	"io"
	"parrot/internal/engine/stream"
)

// Terminal is /dev/tty
type Terminal struct {
	in  io.Reader
	out io.Writer
}

func NewTerminal(in io.Reader, out io.Writer) *Terminal {
	return &Terminal{in: in, out: out}
}

// MayBlock is true: the terminal is a Buffer that holds writes while the
// session has no output, and a read waits on the keyboard.
func (t *Terminal) MayBlock() bool { return true }

func (t *Terminal) Read(p []byte) (int, error) { return t.in.Read(p) }

func (t *Terminal) Write(p []byte) (int, error) { return t.out.Write(p) }

// Stderr is /dev/stderr, the opener's fd 2.
func Stderr() Descriptor { return Descriptor{fd: stream.Stderr} }

// Stdin is /dev/stdin, the opener's fd 0.
func Stdin() Descriptor { return Descriptor{fd: stream.Stdin} }

// Stdout is /dev/stdout, the opener's fd 1.
func Stdout() Descriptor { return Descriptor{fd: stream.Stdout} }
