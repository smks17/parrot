package osfs

import (
	"os"
	"path/filepath"

	"parrot/internal/engine/filesystem"
	"parrot/internal/engine/stream"
)

func (f *FS) Stat(path string) (filesystem.Info, error) {
	target := f.resolve(path)
	stat, err := os.Stat(target)
	if err != nil {
		return filesystem.Info{}, translate(err)
	}
	return describe(target, stat), nil
}

func (f *FS) List(path string) ([]filesystem.Info, error) {
	target := f.resolve(path)
	stat, err := os.Stat(target)
	if err != nil {
		return nil, translate(err)
	}
	if !stat.IsDir() {
		return []filesystem.Info{describe(target, stat)}, nil
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, translate(err)
	}
	out := make([]filesystem.Info, 0, len(entries))
	for _, entry := range entries {
		stat, err := entry.Info()
		if err != nil {
			continue // it went away between the listing and the stat
		}
		out = append(out, describe(filepath.Join(target, entry.Name()), stat))
	}
	return out, nil
}

func (f *FS) Walk(path string, do func(filesystem.Info) error) error {
	root := f.resolve(path)
	return filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return translate(err)
		}
		stat, err := entry.Info()
		if err != nil {
			return translate(err)
		}
		return do(describe(name, stat))
	})
}

func (f *FS) OpenDefault(path string) (*stream.File, error) {
	return f.Open(path, stream.O_RDONLY)
}

func (f *FS) Open(path string, flags stream.OpenFlags) (*stream.File, error) {
	target := f.resolve(path)

	// A directory is read with List, not with read(2), as in the in-memory tree.
	if stat, err := os.Stat(target); err == nil && stat.IsDir() {
		return nil, filesystem.ErrIsDir
	}

	var host int
	switch {
	case flags.Readable() && flags.Writable():
		host = os.O_RDWR
	case flags.Writable():
		host = os.O_WRONLY
	default:
		host = os.O_RDONLY
	}
	if flags&stream.O_CREATE != 0 {
		host |= os.O_CREATE
	}
	if flags&stream.O_TRUNC != 0 && flags.Writable() {
		host |= os.O_TRUNC
	}
	if flags&stream.O_APPEND != 0 {
		host |= os.O_APPEND
	}

	file, err := os.OpenFile(target, host, os.FileMode(filesystem.DefaultFileMode))
	if err != nil {
		return nil, translate(err)
	}
	return stream.NewHostFile(file, flags), nil
}

func (f *FS) Read(path string) ([]byte, error) {
	content, err := os.ReadFile(f.resolve(path))
	if err != nil {
		return nil, translate(err)
	}
	return content, nil
}

// describe renders a real entry in the shape the rest of the program uses.
func describe(path string, stat os.FileInfo) filesystem.Info {
	mode := filesystem.FileMode(stat.Mode().Perm())
	if stat.IsDir() {
		mode |= filesystem.ModeDirectory
	}

	size := stat.Size()
	if stat.IsDir() {
		size = 0
	}
	return filesystem.Info{
		Name:    stat.Name(),
		Path:    path,
		Mode:    mode,
		Owner:   owner(stat),
		Size:    size,
		Links:   linkCount(stat),
		ModTime: stat.ModTime(),
	}
}
