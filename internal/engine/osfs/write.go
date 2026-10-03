package osfs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"parrot/internal/engine/filesystem"
)

func (f *FS) Write(path string, content []byte, appending bool) error {
	flags := os.O_WRONLY | os.O_CREATE
	if appending {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	file, err := os.OpenFile(f.resolve(path), flags, os.FileMode(filesystem.DefaultFileMode))
	if err != nil {
		return translate(err)
	}
	defer file.Close()

	if _, err := file.Write(content); err != nil {
		return translate(err)
	}
	return nil
}

// Create makes an empty file and, like the in-memory tree, reports one that
// is already there rather than emptying it.
func (f *FS) Create(path string) error {
	file, err := os.OpenFile(f.resolve(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(filesystem.DefaultFileMode))
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return filesystem.ErrExists
		}
		return translate(err)
	}
	return file.Close()
}

func (f *FS) Mkdir(path string) error {
	if err := os.Mkdir(f.resolve(path), os.FileMode(filesystem.DefaultDirMode&0o777)); err != nil {
		return translate(err)
	}
	return nil
}

func (f *FS) Remove(path string, recursive bool) error {
	target := f.resolve(path)
	if target == "/" {
		return filesystem.ErrRootRemove
	}
	if recursive {
		return translate(os.RemoveAll(target))
	}
	return translate(os.Remove(target))
}

func (f *FS) Copy(src, dst string, recursive bool) error {
	from, to := f.resolve(src), f.destination(src, dst)

	stat, err := os.Stat(from)
	if err != nil {
		return translate(err)
	}
	if !stat.IsDir() {
		return copyFile(from, to, stat.Mode())
	}
	if !recursive {
		return filesystem.ErrIsDir
	}
	return copyTree(from, to)
}

func (f *FS) Move(src, dst string) error {
	from, to := f.resolve(src), f.destination(src, dst)
	if from == "/" {
		return filesystem.ErrRootRemove
	}
	if err := os.Rename(from, to); err == nil {
		return nil
	}

	return filesystem.MoveViaCopy(f, src, dst)
}

// destination resolves the target of cp and mv: naming a directory means
// "into it", as it does everywhere else.
func (f *FS) destination(src, dst string) string {
	target := f.resolve(dst)
	if stat, err := os.Stat(target); err == nil && stat.IsDir() {
		return filepath.Join(target, filesystem.NameFor(src))
	}
	return target
}

// Touch creates a file that is not there, and otherwise moves its modified
// time to now — which is what the command of that name is for.
func (f *FS) Touch(path string) error {
	created, err := filesystem.CreateMissing(f, path)
	if err != nil || created {
		return err
	}
	// It was already there, so this is only a new timestamp — the machine's
	// own, since these are real files.
	now := time.Now()
	return translate(os.Chtimes(f.resolve(path), now, now))
}

func (f *FS) Chmod(path string, mode filesystem.FileMode) error {
	return translate(os.Chmod(f.resolve(path), os.FileMode(mode&0o777)))
}

func (f *FS) Chown(path string, owner, group string) error {
	uid, gid, err := ids(owner, group)
	if err != nil {
		return err
	}
	return translate(os.Chown(f.resolve(path), uid, gid))
}

func copyFile(from, to string, mode os.FileMode) error {
	source, err := os.Open(from)
	if err != nil {
		return translate(err)
	}
	defer source.Close()

	target, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return translate(err)
	}
	defer target.Close()

	if _, err := io.Copy(target, source); err != nil {
		return translate(err)
	}
	return nil
}

func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return translate(err)
		}
		relative, err := filepath.Rel(from, name)
		if err != nil {
			return err
		}
		target := filepath.Join(to, relative)

		stat, err := entry.Info()
		if err != nil {
			return translate(err)
		}
		if entry.IsDir() {
			if err := os.MkdirAll(target, stat.Mode().Perm()); err != nil {
				return translate(err)
			}
			return nil
		}
		return copyFile(name, target, stat.Mode())
	})
}
