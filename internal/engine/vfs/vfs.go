package vfs

import (
	"errors"
	"strings"

	"parrot/internal/engine/dev"
	"parrot/internal/engine/filesystem"
	"parrot/internal/engine/stream"
	"parrot/internal/engine/user"
)

var _ filesystem.FS = (*VFS)(nil)

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
	seedDev(root)

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
		club.setOwner(Ownership{User: user.RootName, Group: user.DevGroup}, 0770|filesystem.ModeDirectory)
	}

	// /home is created as root on the way to the first home under it.
	for _, id := range f.db.Accounts() {
		mode := filesystem.FileMode(0755) | filesystem.ModeDirectory
		if id.IsRoot() {
			mode = 0700 | filesystem.ModeDirectory // nobody reads root's home but root
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

func makeHome(root *Inode, id user.Identity, mode filesystem.FileMode) {
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
	seedDev(root) // a snapshot never holds devices, so they are made again
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

func (f *VFS) Group() string { return f.id.Primary() }

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
			return nil, filesystem.ErrNotDir
		}
		// Looking a name up inside a directory needs execute on it
		if err := f.checkPerm(current, filesystem.PermExec); err != nil {
			return nil, err
		}
		next, ok := current.Lookup(seg)
		if !ok {
			return nil, filesystem.ErrNotExist
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
		return filesystem.ErrNotDir
	}
	// Entering a directory needs execute on the directory itself
	if err := f.checkPerm(dir, filesystem.PermExec); err != nil {
		return err
	}
	f.cwd = dir
	return nil
}

func (f *VFS) splitPath(p string) (parent *Inode, name string, err error) {
	trimmed := strings.TrimRight(p, "/")
	if trimmed == "" {
		return nil, "", filesystem.ErrInvalid // the root is nobody's entry
	}
	parentPath, name := ".", trimmed
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		parentPath, name = trimmed[:i], trimmed[i+1:]
		if parentPath == "" {
			parentPath = "/"
		}
	}
	if name == currDir || name == preDir {
		return nil, "", filesystem.ErrInvalid
	}

	parent, err = f.Resolve(parentPath)
	if err != nil {
		return nil, "", err
	}
	if !parent.IsDir() {
		return nil, "", filesystem.ErrNotDir
	}
	return parent, name, nil
}

func (f *VFS) splitParent(p string) (parent *Inode, name string, err error) {
	parent, name, err = f.splitPath(p)
	if err != nil {
		if errors.Is(err, filesystem.ErrInvalid) {
			return nil, "", filesystem.ErrExists // the root always exists
		}
		return nil, "", err
	}
	if _, exists := parent.Lookup(name); exists {
		return nil, "", filesystem.ErrExists
	}
	return parent, name, nil
}

func (f *VFS) OpenDefault(p string) (*stream.File, error) {
	return f.Open(p, stream.O_RDONLY)
}

func (f *VFS) Open(p string, flags stream.OpenFlags) (*stream.File, error) {
	return f.OpenFor(p, flags, nil)
}

func (f *VFS) OpenFor(p string, flags stream.OpenFlags, fds *stream.FDTable) (*stream.File, error) {
	node, err := f.Resolve(p)
	if err != nil {
		if !errors.Is(err, filesystem.ErrNotExist) || flags&stream.O_CREATE == 0 {
			return nil, err
		}
		if err := f.Create(p); err != nil {
			return nil, err
		}
		if node, err = f.Resolve(p); err != nil {
			return nil, err
		}
	}
	var need filesystem.PermBits
	if flags.Readable() {
		need |= filesystem.PermRead
	}
	if flags.Writable() {
		need |= filesystem.PermWrite
	}
	if err := f.checkPerm(node, need); err != nil {
		return nil, err
	}
	// A device has no bytes to truncate or append to, so those flags mean
	// nothing to it, as they mean nothing to /dev/null on Linux.
	switch device := node.Device().(type) {
	case nil:
		return stream.OpenFile(node, flags)
	case dev.Descriptor:
		return f.reopen(device, flags, need, fds)
	default:
		return stream.NewDeviceFile(device, flags), nil
	}
}

func (f *VFS) reopen(device dev.Descriptor, flags stream.OpenFlags, need filesystem.PermBits, fds *stream.FDTable) (*stream.File, error) {
	target, err := device.Target(fds)
	if errors.Is(err, stream.ErrBadFD) {
		return nil, filesystem.ErrNotExist // fd N is closed: the link dangles
	}
	if err != nil {
		return nil, err
	}
	if node, ok := target.Store().(*Inode); ok {
		if err := f.checkPerm(node, need); err != nil {
			return nil, err
		}
		return stream.OpenFile(node, flags)
	}
	return stream.NewDeviceFile(target, flags), nil
}

func (f *VFS) MakeDevice(p string, device dev.Device, perm filesystem.FileMode) error {
	parent, name, err := f.splitPath(p)
	if err != nil {
		return err
	}
	if old, ok := parent.Lookup(name); ok {
		if old.Device() == nil {
			return filesystem.ErrExists // never replace a real file
		}
		if err := parent.Unlink(name); err != nil {
			return err
		}
	}
	return parent.Link(name, NewDevice(device, rootOwner(), perm))
}

func (f *VFS) create(p string, node *Inode) error {
	parent, name, err := f.splitParent(p)
	if err != nil {
		return err
	}
	// Creating an entry is a write on the parent directory + the search to reach into it
	if err := f.checkPerm(parent, filesystem.PermWrite|filesystem.PermExec); err != nil {
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
		return filesystem.ErrLinkDir
	}
	parent, name, err := f.destination(oldPath, newPath)
	if err != nil {
		return err
	}
	// Creating an entry is a write on the parent directory + the search to reach into it
	if err := f.checkPerm(parent, filesystem.PermWrite|filesystem.PermExec); err != nil {
		return err
	}
	return parent.Link(name, node)
}

func (f *VFS) Write(p string, b []byte, writeAppend bool) error {
	node, err := f.Resolve(p)
	if err != nil {
		return err
	}
	if err := f.checkPerm(node, filesystem.PermWrite); err != nil {
		return err
	}
	if node.IsDir() {
		return filesystem.ErrIsDir
	}
	if node.Device() != nil {
		file, err := f.Open(p, stream.O_WRONLY)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = file.Write(b)
		return err
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
	if err := f.checkPerm(node, filesystem.PermRead); err != nil {
		return nil, err
	}
	if node.IsDir() {
		return nil, filesystem.ErrIsDir
	}
	if node.Device() != nil {
		return nil, filesystem.ErrNotRegular // /dev/zero has no whole content
	}
	return node.Bytes(), nil
}

func info(p string, node *Inode) filesystem.Info {
	owner := node.Owner()
	id := user.Identity{Name: owner.User}
	if owner.Group != "" {
		id.Groups = []user.Group{{Name: owner.Group}}
	}
	return filesystem.Info{
		Name:    filesystem.NameFor(p),
		Ino:     uint64(node.Ino()),
		Path:    p,
		Size:    node.Size(),
		Mode:    node.Mode(),
		Owner:   id,
		Links:   node.Nlink(),
		ModTime: node.ModTime(),
	}
}

// Stat describes one entry without reading it.
func (f *VFS) Stat(p string) (filesystem.Info, error) {
	node, err := f.Resolve(p)
	if err != nil {
		return filesystem.Info{}, err
	}
	return info(p, node), nil
}

// Walk visits a path and everything under it. The path is carried down rather
// than asked of each inode, which has no name of its own.
func (f *VFS) Walk(p string, do func(filesystem.Info) error) error {
	node, err := f.Resolve(p)
	if err != nil {
		return err
	}
	return node.Walk(p, func(at string, n *Inode) error { return do(info(at, n)) })
}

func (f *VFS) List(p string) ([]filesystem.Info, error) {
	node, err := f.Resolve(p)
	if err != nil {
		return nil, err
	}
	if !node.IsDir() {
		return []filesystem.Info{info(p, node)}, nil
	}
	// Reading the names inside a directory is the directory's read bit.
	if err := f.checkPerm(node, filesystem.PermRead); err != nil {
		return nil, err
	}
	dir := strings.TrimRight(p, "/")
	entries := make([]filesystem.Info, 0, node.NumEntries())
	for _, entry := range node.Entries() {
		entries = append(entries, info(dir+"/"+entry.Name, entry.Inode))
	}
	return entries, nil
}

func (f *VFS) Remove(p string, recursive bool) error {
	parent, name, err := f.splitPath(p)
	if err != nil {
		if errors.Is(err, filesystem.ErrInvalid) {
			// The root is no directory's entry, and "rm -rf /" should not empty
			// the world.
			return filesystem.ErrRootRemove
		}
		return err
	}
	node, ok := parent.Lookup(name)
	if !ok {
		return filesystem.ErrNotExist
	}
	// Unlinking is a write on the directory the entry lives in
	if err := f.checkPerm(parent, filesystem.PermWrite|filesystem.PermExec); err != nil {
		return err
	}
	if node.IsDir() && node.NumEntries() > 0 && !recursive {
		return filesystem.ErrNotEmptyDir
	}
	return parent.Unlink(name)
}

func (f *VFS) destination(src, dst string) (parent *Inode, name string, err error) {
	if node, err := f.Resolve(dst); err == nil && node.IsDir() {
		name := filesystem.NameFor(src)
		if _, exists := node.Lookup(name); exists {
			return nil, "", filesystem.ErrExists
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
	// Without -r a directory is refused whether or not it is empty, which is
	// what cp does and what the real filesystem already said.
	if !recursive && node.IsDir() {
		return filesystem.ErrIsDir
	}
	if node.Device() != nil {
		return filesystem.ErrNotRegular
	}
	// Copying reads the source's content.
	if err := f.checkPerm(node, filesystem.PermRead); err != nil {
		return err
	}
	parent, name, err := f.destination(src, dst)
	if err != nil {
		return err
	}
	// ...and creating the copy is a write on the destination directory.
	if err := f.checkPerm(parent, filesystem.PermWrite|filesystem.PermExec); err != nil {
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
		return filesystem.ErrRootRemove
	}
	srcParent, srcName, err := f.splitPath(src)
	if err != nil {
		return err
	}
	// A rename is a write on the directory the entry leaves + one on the directory it lands in
	if err := f.checkPerm(srcParent, filesystem.PermWrite|filesystem.PermExec); err != nil {
		return err
	}
	parent, name, err := f.destination(src, dst)
	if err != nil {
		return err
	}
	if err := f.checkPerm(parent, filesystem.PermWrite|filesystem.PermExec); err != nil {
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

func (f *VFS) Chmod(p string, mode filesystem.FileMode) error {
	node, err := f.Resolve(p)
	if err != nil {
		return err
	}
	return f.ChmodNode(node, mode)
}

func (f *VFS) ChmodNode(n *Inode, mode filesystem.FileMode) error {
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
		return filesystem.ErrNotExist
	}

	if owner != "" && !f.db.UserExists(owner) {
		return user.ErrNoUser(owner)
	}
	if group != "" && !f.db.GroupExists(group) {
		return user.ErrNoGroup(group)
	}

	if !f.id.IsRoot() {
		if owner != "" {
			return filesystem.ErrNotOwner
		}
		if group != "" && (n.Owner().User != f.id.Name || !f.id.InGroup(group)) {
			return filesystem.ErrNotOwner
		}
	}

	n.chown(owner, group)
	return nil
}

func (f *VFS) Touch(path string) error {
	created, err := filesystem.CreateMissing(f, path)
	if err != nil || created {
		return err
	}
	// It was already there, so this is only a new timestamp — the tree's own,
	// which "date -s" can move.
	node, err := f.Resolve(path)
	if err != nil {
		return err
	}
	if err := f.checkPerm(node, filesystem.PermWrite); err != nil {
		return err
	}
	node.Touch()
	return nil
}
