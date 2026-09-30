package vfs

import (
	"sort"
	"strings"

	"parrot/internal/engine/filesystem"
	"parrot/internal/engine/user"
)

var _ filesystem.FS = (*VFS)(nil)

type VFS struct {
	root *Node
	cwd  *Node
	id   user.Identity
	db   *user.DB
}

func New(users []*user.Identity, rootUser, asUser *user.Identity) *VFS {
	root := NewDir("", *rootUser)

	for _, dir := range []string{
		"home", "books", "club", "projects", "memories", "downloads", "secrets",
	} {
		root.AddChild(NewDir(dir, *asUser))
	}

	home := root.Children["home"]
	for _, user := range users {
		home.AddChild(NewDir(user.Home, *user))
	}

	root.AddChild(NewFile("README.md", []byte(
		"Welcome.\n\nYou are in a terminal that isn't quite real.\nTry: ls, cd, cat\n",
	), *rootUser))

	seedEtc(root, rootUser)

	return newVFS(root, home.Children[asUser.Home])
}

func FromRoot(root *Node, cwd string, rootUser *user.Identity) *VFS {
	seedEtc(root, rootUser)

	f := newVFS(root, root)
	if dir, err := f.Resolve(cwd); err == nil && dir.IsDir() {
		f.cwd = dir
	}
	return f
}

func newVFS(root, cwd *Node) *VFS {
	return &VFS{root: root, cwd: cwd, db: user.NewDB(accounts{root: root})}
}

func (f *VFS) UsersDB() *user.DB { return f.db }

func (f *VFS) SetIdentity(id user.Identity) { f.id = id }

func (f *VFS) Identity() user.Identity { return f.id }

func (f *VFS) RootUser() (user.Identity, error) { return f.db.Root() } //TODO: Delete later

func (f *VFS) User() string { return f.id.Name }

func (f *VFS) Group() string { return f.id.Primary() }

func (f *VFS) Cwd() string {
	return f.cwd.Path()
}

func (f *VFS) RootNode() *Node {
	return f.root
}

func (f *VFS) Resolve(path string) (*Node, error) {
	curr := f.cwd
	if strings.HasPrefix(path, "/") {
		curr = f.root
	}

	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." {
			continue
		}

		if seg == ".." {
			if curr.Parent != nil {
				curr = curr.Parent
			}
			continue
		}

		if !curr.IsDir() {
			return nil, filesystem.ErrNotDir
		}

		// Looking up a name inside a directory needs execute on it
		if err := f.checkPerm(curr, filesystem.PermExec); err != nil {
			return nil, err
		}

		next, ok := curr.Children[seg]
		if !ok {
			return nil, filesystem.ErrNotExist
		}
		curr = next
	}

	return curr, nil
}

func (f *VFS) Chdir(path string) error {
	dir, err := f.Resolve(path)
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

func (f *VFS) splitParent(path string) (parent *Node, name string, err error) {
	parentPath, name := filesystem.Split(path)
	if name == "" {
		return nil, "", filesystem.ErrExists // the root always exists
	}

	parent, err = f.Resolve(parentPath)
	if err != nil {
		return nil, "", err
	}
	if !parent.IsDir() {
		return nil, "", filesystem.ErrNotDir
	}
	if _, exists := parent.Children[name]; exists {
		return nil, "", filesystem.ErrExists
	}
	return parent, name, nil
}

func (f *VFS) Mkdir(path string) error {
	parent, name, err := f.splitParent(path)
	if err != nil {
		return err
	}
	// Creating an entry is a write on the parent directory + the search to reach into it
	if err := f.checkPerm(parent, filesystem.PermWrite|filesystem.PermExec); err != nil {
		return err
	}
	parent.AddChild(NewDir(name, f.id))
	return nil
}

func (f *VFS) Create(path string) error {
	parent, name, err := f.splitParent(path)
	if err != nil {
		return err
	}
	// Creating an entry is a write on the parent directory + the search to reach into it
	if err := f.checkPerm(parent, filesystem.PermWrite|filesystem.PermExec); err != nil {
		return err
	}
	parent.AddChild(NewFile(name, nil, f.id))
	return nil
}

func (f *VFS) Write(path string, b []byte, writeAppend bool) error {
	nodeFile, err := f.Resolve(path)
	if err != nil {
		return err
	}
	if err := f.checkPerm(nodeFile, filesystem.PermWrite); err != nil {
		return err
	}
	if nodeFile.IsDir() {
		return filesystem.ErrIsDir
	}
	if writeAppend {
		nodeFile.Append(b)
	} else {
		nodeFile.Override(b)
	}
	return nil

}

func (f *VFS) Read(path string) ([]byte, error) {
	nodeFile, err := f.Resolve(path)
	if err != nil {
		return nil, err
	}
	if err := f.checkPerm(nodeFile, filesystem.PermRead); err != nil {
		return nil, err
	}
	if nodeFile.IsDir() {
		return nil, filesystem.ErrIsDir
	}
	return nodeFile.Content, nil
}

// Stat describes one entry without reading it.
func (f *VFS) Stat(path string) (filesystem.Info, error) {
	node, err := f.Resolve(path)
	if err != nil {
		return filesystem.Info{}, err
	}
	return info(node), nil
}

// Walk visits a path and everything under it.
func (f *VFS) Walk(path string, do func(filesystem.Info) error) error {
	node, err := f.Resolve(path)
	if err != nil {
		return err
	}
	return node.Walk(func(n *Node) error { return do(info(n)) })
}

func (f *VFS) List(path string) ([]filesystem.Info, error) {
	nodeFile, err := f.Resolve(path)
	if err != nil {
		return nil, err
	}
	if !nodeFile.IsDir() {
		return []filesystem.Info{info(nodeFile)}, nil
	}
	// Reading the names inside a directory is the directory's read bit.
	if err := f.checkPerm(nodeFile, filesystem.PermRead); err != nil {
		return nil, err
	}
	var sortedNode []*Node
	for _, node := range nodeFile.Children {
		sortedNode = append(sortedNode, node)
	}
	sort.Slice(sortedNode, func(i, j int) bool {
		return sortedNode[i].Name < sortedNode[j].Name
	})

	entries := make([]filesystem.Info, 0, len(sortedNode))
	for _, node := range sortedNode {
		entries = append(entries, info(node))
	}
	return entries, nil
}

func (f *VFS) Remove(path string, recursive bool) error {
	node, err := f.Resolve(path)
	if err != nil {
		return err
	}

	// The root has no parent to unlink from, and "rm -rf /" should not empty
	// the world.
	if node.Parent == nil {
		return filesystem.ErrRootRemove
	}
	// Unlinking is a write on the directory the entry lives in
	if err := f.checkPerm(node.Parent, filesystem.PermWrite|filesystem.PermExec); err != nil {
		return err
	}
	if node.IsDir() && len(node.Children) > 0 && !recursive {
		return filesystem.ErrNotEmptyDir
	}
	node.Parent.removeChild(node.Name)
	return nil
}

func (f *VFS) destination(src, dst string) (parent *Node, name string, err error) {
	if dstNode, err := f.Resolve(dst); err == nil && dstNode.IsDir() {
		return dstNode, filesystem.NameFor(src), nil
	}
	return f.splitParent(dst)
}

func (f *VFS) Copy(src, dst string, recursive bool) error {
	srcNode, err := f.Resolve(src)
	if err != nil {
		return err
	}
	// Without -r a directory is refused whether or not it is empty, which is
	// what cp does and what the real filesystem already said.
	if !recursive && srcNode.IsDir() {
		return filesystem.ErrIsDir
	}
	// Copying reads the source's content.
	if err := f.checkPerm(srcNode, filesystem.PermRead); err != nil {
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
	newNode := srcNode.Clone()
	newNode.Name = name
	// TODO: It should be handle in node.go
	newNode.Walk(func(n *Node) error {
		n.Touch()
		return nil
	})
	parent.AddChild(newNode)
	return nil
}

func (f *VFS) Move(src, dst string) error {
	srcNode, err := f.Resolve(src)
	if err != nil {
		return err
	}
	if srcNode.Path() == "/" {
		return filesystem.ErrRootRemove
	}
	// A rename is a write on the directory the entry leaves + one on the directory it lands in
	if err := f.checkPerm(srcNode.Parent, filesystem.PermWrite|filesystem.PermExec); err != nil {
		return err
	}
	parent, name, err := f.destination(src, dst)
	if err != nil {
		return err
	}
	if err := f.checkPerm(parent, filesystem.PermWrite|filesystem.PermExec); err != nil {
		return err
	}
	srcNode.Parent.removeChild(srcNode.Name)
	srcNode.Name = name
	parent.AddChild(srcNode)
	return nil
}

func (f *VFS) Chmod(path string, mode filesystem.FileMode) error {
	node, err := f.Resolve(path)
	if err != nil {
		return err
	}
	return f.ChmodNode(node, mode)
}

func (f *VFS) ChmodNode(n *Node, mode filesystem.FileMode) error {
	if err := f.checkOwnership(n); err != nil {
		return err
	}
	// Clear existing permission bits
	n.Mode &= ^filesystem.FileMode(0777)
	// Set new permission bits
	n.Mode |= mode & 0777
	return nil
}

func (f *VFS) Chown(path string, owner user.Identity) error {
	node, err := f.Resolve(path)
	if err != nil {
		return err
	}
	return f.ChownNode(node, owner)
}

func (f *VFS) ChownNode(n *Node, owner user.Identity) error {
	if err := f.checkOwnership(n); err != nil {
		return err
	}
	n.Owner = owner
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
