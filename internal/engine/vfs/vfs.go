package vfs

import (
	"errors"
	"path"
	"strings"

	"parrot/internal/engine/user"
)

type VFS struct {
	root *Inode
	cwd  *Inode
	id   user.Identity
	db   *user.DB
}

func rootOwner() Ownership { return Ownership{User: user.RootName, Group: user.RootGroup} }

func New() *VFS {
	root := newRoot(rootOwner())
	seedEtc(root)

	f := newVFS(root, root)
	resident, err := f.db.Resident()
	residentOwner := rootOwner()
	if err == nil {
		residentOwner = OwnedBy(resident)
	}

	for _, dir := range []string{
		"books", "club", "projects", "memories", "downloads", "secrets",
	} {
		_ = root.Link(dir, NewDir(residentOwner))
	}
	_ = root.Link("README.md", NewFile([]byte(
		"Welcome.\n\nYou are in a terminal that isn't quite real.\nTry: ls, cd, cat\n",
	), rootOwner()))
	if club, ok := root.Lookup("club"); ok {
		club.setOwner(Ownership{user.RootName, user.DevGroup}, 0770|ModeDirectory)
	}

	// /home is created as root on the way to the first home under it.
	for _, id := range f.db.Accounts() {
		mode := FileMode(0755) | ModeDirectory
		if id.IsRoot() {
			mode = 0700 | ModeDirectory // nobody reads root's home but root
		}
		makeHome(root, id, mode)
	}

	if err == nil {
		if home, err := f.Resolve(resident.Home); err == nil && home.IsDir() {
			f.cwd = home
		}
	}
	return f
}

func makeHome(root *Inode, id user.Identity, mode FileMode) {
	segments := strings.Split(strings.Trim(id.Home, "/"), "/")
	if len(segments) == 1 && segments[0] == "" {
		return // an account living at / has nothing to create
	}
	dir := root
	for _, seg := range segments[:len(segments)-1] {
		next, ok := dir.Lookup(seg)
		if !ok {
			next = NewDir(rootOwner())
			if err := dir.Link(seg, next); err != nil {
				return
			}
		}
		if !next.IsDir() {
			return // a file sits where this home needs a directory
		}
		dir = next
	}
	home := NewDir(OwnedBy(id)).setOwner(OwnedBy(id), mode)
	_ = dir.Link(segments[len(segments)-1], home)
}

func FromRoot(root *Inode, cwd string) *VFS {
	f := newVFS(root, root)
	if dir, err := f.Resolve(cwd); err == nil && dir.IsDir() {
		f.cwd = dir
	}
	return f
}

func newVFS(root, cwd *Inode) *VFS {
	return &VFS{root: root, cwd: cwd, db: user.NewDB(accounts{root: root})}
}

func (f *VFS) UsersDB() *user.DB { return f.db }

func (f *VFS) SetIdentity(id user.Identity) { f.id = id }

func (f *VFS) Identity() user.Identity { return f.id }

func (f *VFS) RootUser() (user.Identity, error) { return f.db.Root() } //TODO: Delete later

func (f *VFS) User() string { return f.id.Name }

func (f *VFS) Cwd() string { return f.cwd.Path() }

func (f *VFS) RootInode() *Inode { return f.root }

func (f *VFS) Resolve(p string) (*Inode, error) {
	current := f.cwd
	if strings.HasPrefix(p, "/") {
		current = f.root
	}

	for _, seg := range strings.Split(p, "/") {
		if seg == "" {
			continue
		}
		if !current.IsDir() {
			return nil, ErrNotDir
		}
		// Looking a name up inside a directory needs execute on it
		if err := f.checkPerm(current, permExec); err != nil {
			return nil, err
		}
		next, ok := current.Lookup(seg)
		if !ok {
			return nil, ErrNotExist
		}
		current = next
	}

	return current, nil
}

func (f *VFS) Chdir(p string) error {
	dir, err := f.Resolve(p)
	if err != nil {
		return err
	}
	if !dir.IsDir() {
		return ErrNotDir
	}
	// Entering a directory needs execute on the directory itself
	if err := f.checkPerm(dir, permExec); err != nil {
		return err
	}
	f.cwd = dir
	return nil
}

func (f *VFS) splitPath(p string) (parent *Inode, name string, err error) {
	trimmed := strings.TrimRight(p, "/")
	if trimmed == "" {
		return nil, "", ErrInvalid // the root is nobody's entry
	}
	parentPath, name := ".", trimmed
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		parentPath, name = trimmed[:i], trimmed[i+1:]
		if parentPath == "" {
			parentPath = "/"
		}
	}
	if name == currDir || name == preDir {
		return nil, "", ErrInvalid
	}

	parent, err = f.Resolve(parentPath)
	if err != nil {
		return nil, "", err
	}
	if !parent.IsDir() {
		return nil, "", ErrNotDir
	}
	return parent, name, nil
}

func (f *VFS) splitParent(p string) (parent *Inode, name string, err error) {
	parent, name, err = f.splitPath(p)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			return nil, "", ErrExists // the root always exists
		}
		return nil, "", err
	}
	if _, exists := parent.Lookup(name); exists {
		return nil, "", ErrExists
	}
	return parent, name, nil
}

func (f *VFS) OpenDefault(p string) (*File, error) {
	return f.Open(p, O_RDONLY)
}

func (f *VFS) Open(p string, flags OpenFlags) (*File, error) {
	node, err := f.Resolve(p)
	if err != nil {
		if !errors.Is(err, ErrNotExist) || flags&O_CREATE == 0 {
			return nil, err
		}
		if err := f.Create(p); err != nil {
			return nil, err
		}
		if node, err = f.Resolve(p); err != nil {
			return nil, err
		}
	}
	var need permBits
	if flags.readable() {
		need |= permRead
	}
	if flags.writable() {
		need |= permWrite
	}
	if err := f.checkPerm(node, need); err != nil {
		return nil, err
	}
	return OpenFile(node, flags)
}

func (f *VFS) create(p string, node *Inode) error {
	parent, name, err := f.splitParent(p)
	if err != nil {
		return err
	}
	if err := f.checkPerm(parent, permWrite|permExec); err != nil {
		return err
	}
	return parent.Link(name, node)
}

func (f *VFS) Mkdir(p string) error { return f.create(p, NewDir(OwnedBy(f.id))) }

func (f *VFS) Create(p string) error { return f.create(p, NewFile(nil, OwnedBy(f.id))) }

// Link is link(2): one more name for an inode that already exists. Directories
// are refused, as they are on Linux, because a cycle of them would leave the
// tree with no way out.
func (f *VFS) Link(oldPath, newPath string) error {
	node, err := f.Resolve(oldPath)
	if err != nil {
		return err
	}
	if node.IsDir() {
		return ErrLinkDir
	}
	parent, name, err := f.destination(oldPath, newPath)
	if err != nil {
		return err
	}
	if err := f.checkPerm(parent, permWrite|permExec); err != nil {
		return err
	}
	return parent.Link(name, node)
}

func (f *VFS) Write(p string, b []byte, writeAppend bool) error {
	node, err := f.Resolve(p)
	if err != nil {
		return err
	}
	if err := f.checkPerm(node, permWrite); err != nil {
		return err
	}
	if node.IsDir() {
		return ErrIsDir
	}
	if writeAppend {
		node.Append(b)
	} else {
		node.Override(b)
	}
	return nil
}

func (f *VFS) Read(p string) ([]byte, error) {
	node, err := f.Resolve(p)
	if err != nil {
		return nil, err
	}
	if err := f.checkPerm(node, permRead); err != nil {
		return nil, err
	}
	if node.IsDir() {
		return nil, ErrIsDir
	}
	return node.Bytes(), nil
}

// List reads a directory. Reading the names in one is its read bit, where
// reaching a name through it is its execute bit.
func (f *VFS) List(p string) ([]Dirent, error) {
	node, err := f.Resolve(p)
	if err != nil {
		return nil, err
	}
	if !node.IsDir() {
		return []Dirent{{Name: path.Base(strings.TrimRight(p, "/")), Inode: node}}, nil
	}
	if err := f.checkPerm(node, permRead); err != nil {
		return nil, err
	}
	return node.Entries(), nil
}

func (f *VFS) Remove(p string, recursive bool) error {
	parent, name, err := f.splitPath(p)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			// The root is no directory's entry, and "rm -rf /" should not empty
			// the world.
			return ErrRootRemove
		}
		return err
	}
	node, ok := parent.Lookup(name)
	if !ok {
		return ErrNotExist
	}
	// Unlinking is a write on the directory the entry lives in
	if err := f.checkPerm(parent, permWrite|permExec); err != nil {
		return err
	}
	if node.IsDir() && node.NumEntries() > 0 && !recursive {
		return ErrNotEmptyDir
	}
	return parent.Unlink(name)
}

func (f *VFS) destination(src, dst string) (parent *Inode, name string, err error) {
	if node, err := f.Resolve(dst); err == nil && node.IsDir() {
		name := path.Base(strings.TrimRight(src, "/"))
		if _, exists := node.Lookup(name); exists {
			return nil, "", ErrExists
		}
		return node, name, nil
	}
	return f.splitParent(dst)
}

func (f *VFS) Copy(src, dst string, recursive bool) error {
	node, err := f.Resolve(src)
	if err != nil {
		return err
	}
	if !recursive && node.IsDir() && node.NumEntries() > 0 {
		return ErrNotEmptyDir
	}
	// Copying reads the source's content.
	if err := f.checkPerm(node, permRead); err != nil {
		return err
	}
	parent, name, err := f.destination(src, dst)
	if err != nil {
		return err
	}
	// ...and creating the copy is a write on the destination directory.
	if err := f.checkPerm(parent, permWrite|permExec); err != nil {
		return err
	}
	// A copy is new inodes, not another name for the old ones: that is what
	// separates cp from ln.
	copied := node.Clone()
	_ = copied.Walk("", func(_ string, n *Inode) error {
		n.Touch()
		return nil
	})
	return parent.Link(name, copied)
}

func (f *VFS) Move(src, dst string) error {
	node, err := f.Resolve(src)
	if err != nil {
		return err
	}
	if node == f.root {
		return ErrRootRemove
	}
	srcParent, srcName, err := f.splitPath(src)
	if err != nil {
		return err
	}
	if err := f.checkPerm(srcParent, permWrite|permExec); err != nil {
		return err
	}
	parent, name, err := f.destination(src, dst)
	if err != nil {
		return err
	}
	if err := f.checkPerm(parent, permWrite|permExec); err != nil {
		return err
	}
	if err := srcParent.Unlink(srcName); err != nil {
		return err
	}
	if err := parent.Link(name, node); err != nil {
		// Put it back rather than leave it with no name at all.
		_ = srcParent.Link(srcName, node)
		return err
	}
	return nil
}

func (f *VFS) Chmod(p string, mode FileMode) error {
	node, err := f.Resolve(p)
	if err != nil {
		return err
	}
	return f.ChmodNode(node, mode)
}

func (f *VFS) ChmodNode(n *Inode, mode FileMode) error {
	if err := f.checkOwnership(n); err != nil {
		return err
	}
	n.setPerm(mode)
	return nil
}

func (f *VFS) Chown(p, owner, group string) error {
	node, err := f.Resolve(p)
	if err != nil {
		return err
	}
	return f.ChownNode(node, owner, group)
}

func (f *VFS) ChownNode(n *Inode, owner, group string) error {
	if n == nil {
		return ErrNotExist
	}

	if owner != "" && !f.db.UserExists(owner) {
		return user.ErrNoUser(owner)
	}
	if group != "" && !f.db.GroupExists(group) {
		return user.ErrNoGroup(group)
	}

	if !f.id.IsRoot() {
		if owner != "" {
			return ErrNotOwner
		}
		if group != "" && (n.Owner().User != f.id.Name || !f.id.InGroup(group)) {
			return ErrNotOwner
		}
	}

	n.chown(owner, group)
	return nil
}

func (f *VFS) Touch(p string) error {
	node, err := f.Resolve(p)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return f.Create(p)
		}
		return err
	}
	if err := f.checkPerm(node, permWrite); err != nil {
		return err
	}
	node.Touch()
	return nil
}
