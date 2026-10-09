package shell

import (
	"path"
	"strings"

	"parrot/internal/engine/filesystem"
	"parrot/internal/engine/stream"
)

// dirView is a filesystem with a working directory of its own. A subshell —
// a pipeline stage, a job run with "&" — gets one, so its cd moves only
// itself, as a forked process's would. Without it every shell shares the
// one cwd on the filesystem, and "(cd /tmp; ls) &" would move the prompt.
type dirView struct {
	filesystem.FS
	cwd string
}

func newDirView(fs filesystem.FS) *dirView {
	if view, ok := fs.(*dirView); ok {
		return &dirView{FS: view.FS, cwd: view.cwd}
	}
	return &dirView{FS: fs, cwd: fs.Cwd()}
}

// abs is p as seen from the view's directory. Lexical cleaning is safe here
// because there are no symlinks: ".." always means the parent.
func (v *dirView) abs(p string) string {
	if p == "" {
		return v.cwd
	}
	if strings.HasPrefix(p, "/") {
		return p
	}
	return path.Join(v.cwd, p)
}

func (v *dirView) Cwd() string { return v.cwd }

func (v *dirView) Chdir(p string) error {
	target := v.abs(p)
	info, err := v.FS.Stat(target)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return filesystem.ErrNotDir
	}
	// Entering a directory needs execute on it, as the filesystem's own Chdir checks.
	owner := filesystem.Ownership{User: info.Owner.Name, Group: info.Owner.Primary()}
	if err := filesystem.CheckPerm(info.Mode, owner, v.Identity(), filesystem.PermExec); err != nil {
		return err
	}
	v.cwd = target
	return nil
}

func (v *dirView) Stat(p string) (filesystem.Info, error)   { return v.FS.Stat(v.abs(p)) }
func (v *dirView) List(p string) ([]filesystem.Info, error) { return v.FS.List(v.abs(p)) }
func (v *dirView) Walk(p string, do func(filesystem.Info) error) error {
	return v.FS.Walk(v.abs(p), do)
}

func (v *dirView) Open(p string, flags stream.OpenFlags) (*stream.File, error) {
	return v.FS.Open(v.abs(p), flags)
}
func (v *dirView) OpenDefault(p string) (*stream.File, error) {
	return v.FS.OpenDefault(v.abs(p))
}

func (v *dirView) Read(p string) ([]byte, error) { return v.FS.Read(v.abs(p)) }
func (v *dirView) Write(p string, content []byte, appending bool) error {
	return v.FS.Write(v.abs(p), content, appending)
}
func (v *dirView) Create(p string) error { return v.FS.Create(v.abs(p)) }
func (v *dirView) Mkdir(p string) error  { return v.FS.Mkdir(v.abs(p)) }
func (v *dirView) Remove(p string, recursive bool) error {
	return v.FS.Remove(v.abs(p), recursive)
}
func (v *dirView) Copy(src, dst string, recursive bool) error {
	return v.FS.Copy(v.abs(src), v.abs(dst), recursive)
}
func (v *dirView) Move(src, dst string) error { return v.FS.Move(v.abs(src), v.abs(dst)) }
func (v *dirView) Link(oldpath, newpath string) error {
	return v.FS.Link(v.abs(oldpath), v.abs(newpath))
}
func (v *dirView) Touch(p string) error { return v.FS.Touch(v.abs(p)) }

func (v *dirView) Chmod(p string, mode filesystem.FileMode) error {
	return v.FS.Chmod(v.abs(p), mode)
}
func (v *dirView) Chown(p string, owner, group string) error {
	return v.FS.Chown(v.abs(p), owner, group)
}
