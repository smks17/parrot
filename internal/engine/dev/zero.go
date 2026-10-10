package dev

// /dev/zero
type Zero struct{}

func (Zero) MayBlock() bool { return false }
func (Zero) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
func (Zero) Write(p []byte) (int, error) { return discard(p) }
