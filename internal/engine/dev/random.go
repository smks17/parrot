package dev

import "math/rand/v2"

// /dev/random and /dev/urandom
type Random struct{}

func (Random) MayBlock() bool { return false }
func (Random) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(rand.Uint32())
	}
	return len(p), nil
}
func (Random) Write(p []byte) (int, error) { return discard(p) }
