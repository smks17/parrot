package osfs

import (
	"os"
	"path/filepath"

	"parrot/internal/engine/filesystem"
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
