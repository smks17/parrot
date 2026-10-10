package shell

import (
	"path"
	"strings"

	"parrot/internal/engine/filesystem"
	"parrot/internal/engine/stream"
)

// dirView is the filesystem as one running command sees it, and it carries two
// things: A working directory of its own and the descriptor table of the command
type dirView struct {
	filesystem.FS
	cwd *string
	fds *stream.FDTable
}

func newDirView(fs filesystem.FS) *dirView {
	if view, ok := fs.(*dirView); ok && view.cwd != nil {
		cwd := *view.cwd
		return &dirView{FS: view.FS, cwd: &cwd}
	}
	cwd := fs.Cwd()
	return &dirView{FS: fs, cwd: &cwd}
}

// viewFor is fs as the command with descriptor table fds sees it.
func viewFor(fs filesystem.FS, fds *stream.FDTable) *dirView {
	if view, ok := fs.(*dirView); ok {
		shared := *view
		shared.fds = fds
		return &shared
	}
	return &dirView{FS: fs, fds: fds}
}

// abs is p as seen from the view's directory. Lexical cleaning is safe here
// because there are no symlinks: ".." always means the parent.
func (v *dirView) abs(p string) string {
	if v.cwd == nil {
		return p // the filesystem resolves against its own directory
	}
	if p == "" {
		return *v.cwd
	}
	if strings.HasPrefix(p, "/") {
		return p
	}
	return path.Join(*v.cwd, p)
}

func (v *dirView) Cwd() string {
	if v.cwd == nil {
		return v.FS.Cwd()
	}
	return *v.cwd
}

func (v *dirView) Chdir(p string) error {
	if v.cwd == nil {
		return v.FS.Chdir(p)
	}
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
	*v.cwd = target
	return nil
}

func (v *dirView) Stat(p string) (filesystem.Info, error)   { return v.FS.Stat(v.abs(p)) }
func (v *dirView) List(p string) ([]filesystem.Info, error) { return v.FS.List(v.abs(p)) }
func (v *dirView) Walk(p string, do func(filesystem.Info) error) error {
	return v.FS.Walk(v.abs(p), do)
}

// Open is on behalf of the command this view was made for. OpenFor names the
// table itself, for a redirect that is still building the command's.
func (v *dirView) Open(p string, flags stream.OpenFlags) (*stream.File, error) {
	return v.FS.OpenFor(v.abs(p), flags, v.fds)
}
func (v *dirView) OpenDefault(p string) (*stream.File, error) {
	return v.Open(p, stream.O_RDONLY)
}
func (v *dirView) OpenFor(p string, flags stream.OpenFlags, fds *stream.FDTable) (*stream.File, error) {
	return v.FS.OpenFor(v.abs(p), flags, fds)
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
