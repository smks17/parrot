package filesystem

import "errors"


func CreateMissing(fsys FS, path string) (created bool, err error) {
	_, err = fsys.Stat(path)
	switch {
	case err == nil:
		return false, nil
	case !errors.Is(err, ErrNotExist):
		return false, err
	}
	if err := fsys.Create(path); err != nil {
		return false, err
	}
	return true, nil
}


func MoveViaCopy(fsys FS, src, dst string) error {
	if err := fsys.Copy(src, dst, true); err != nil {
		return err
	}
	return fsys.Remove(src, true)
}
