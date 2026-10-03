package osfs

import (
	"os"
	"path/filepath"

	"parrot/internal/engine/filesystem"
	"parrot/internal/engine/user"
)

type FS struct {
	cwd string
	id  user.Identity
}

var _ filesystem.FS = (*FS)(nil)

func New() (*FS, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return &FS{cwd: cwd, id: current()}, nil
}

func NewAt(dir string) (*FS, error) {
	fs := &FS{cwd: "/", id: current()}
	if err := fs.Chdir(dir); err != nil {
		return nil, err
	}
	return fs, nil
}

func (f *FS) Cwd() string { return f.cwd }

func (f *FS) Identity() user.Identity { return f.id }

func (f *FS) User() string { return f.id.Name }

func (f *FS) Group() string { return f.id.Primary() }

func (f *FS) resolve(path string) string {
	if path == "" {
		return f.cwd
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(f.cwd, path)
}

func (f *FS) Chdir(path string) error {
	target := f.resolve(path)
	info, err := os.Stat(target)
	if err != nil {
		return translate(err)
	}
	if !info.IsDir() {
		return filesystem.ErrNotDir
	}
	f.cwd = target
	return nil
}
