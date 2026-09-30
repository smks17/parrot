package vfs

import (
	"errors"
	"path"
	"sort"
	"strings"

	"parrot/internal/engine/user"
)

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

	// root.setOwner(user.RootName, user.RootGroup, 0755|ModeDirectory)
	// home.setOwner(user.RootName, user.RootGroup, 0755|ModeDirectory)
	// root.Children["README.md"].setOwner(user.RootName, user.RootGroup, 0644)
	// root.AddChild(NewDir(user.RootName).
	// 	setOwner(user.RootName, user.RootGroup, 0700|ModeDirectory))
	// root.Children["club"].setOwner(user.RootName, user.DevGroup, 0770|ModeDirectory)

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
			return nil, ErrNotDir
		}

		// Looking up a name inside a directory needs execute on it
		if err := f.checkPerm(curr, permExec); err != nil {
			return nil, err
		}

		next, ok := curr.Children[seg]
		if !ok {
			return nil, ErrNotExist
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
		return ErrNotDir
	}
	// Entering a directory needs execute on the directory itself
	if err := f.checkPerm(dir, permExec); err != nil {
		return err
	}
	f.cwd = dir
	return nil
}

func (f *VFS) splitParent(path string) (parent *Node, name string, err error) {
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		return nil, "", ErrExists // the root always exists
	}
	parentPath, name := ".", trimmed
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		parentPath, name = trimmed[:i], trimmed[i+1:]
		if parentPath == "" {
			parentPath = "/"
		}
	}

	parent, err = f.Resolve(parentPath)
	if err != nil {
		return nil, "", err
	}
	if !parent.IsDir() {
		return nil, "", ErrNotDir
	}
	if _, exists := parent.Children[name]; exists {
		return nil, "", ErrExists
	}
	return parent, name, nil
}

func (f *VFS) Mkdir(path string) error {
	parent, name, err := f.splitParent(path)
	if err != nil {
		return err
	}
	// Creating an entry is a write on the parent directory + the search to reach into it
	if err := f.checkPerm(parent, permWrite|permExec); err != nil {
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
	if err := f.checkPerm(parent, permWrite|permExec); err != nil {
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
	if err := f.checkPerm(nodeFile, permWrite); err != nil {
		return err
	}
	if nodeFile.IsDir() {
		return ErrIsDir
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
	if err := f.checkPerm(nodeFile, permRead); err != nil {
		return nil, err
	}
	if nodeFile.IsDir() {
		return nil, ErrIsDir
	}
	return nodeFile.Content, nil
}

func (f *VFS) List(path string) ([]*Node, error) {
	nodeFile, err := f.Resolve(path)
	if err != nil {
		return nil, err
	}
	if !nodeFile.IsDir() {
		return []*Node{nodeFile}, nil
	}
	// Reading the names inside a directory is the directory's read bit.
	if err := f.checkPerm(nodeFile, permRead); err != nil {
		return nil, err
	}
	var sortedNode []*Node
	for _, node := range nodeFile.Children {
		sortedNode = append(sortedNode, node)
	}
	sort.Slice(sortedNode, func(i, j int) bool {
		return sortedNode[i].Name < sortedNode[j].Name
	})
	return sortedNode, nil
}

func (f *VFS) Remove(path string, recursive bool) error {
	node, err := f.Resolve(path)
	if err != nil {
		return err
	}

	// The root has no parent to unlink from, and "rm -rf /" should not empty
	// the world.
	if node.Parent == nil {
		return ErrRootRemove
	}
	// Unlinking is a write on the directory the entry lives in
	if err := f.checkPerm(node.Parent, permWrite|permExec); err != nil {
		return err
	}
	if node.IsDir() && len(node.Children) > 0 && !recursive {
		return ErrNotEmptyDir
	}
	node.Parent.removeChild(node.Name)
	return nil
}

func (f *VFS) destination(src, dst string) (parent *Node, name string, err error) {
	if dstNode, err := f.Resolve(dst); err == nil && dstNode.IsDir() {
		return dstNode, path.Base(strings.TrimRight(src, "/")), nil
	}
	return f.splitParent(dst)
}

func (f *VFS) Copy(src, dst string, recursive bool) error {
	srcNode, err := f.Resolve(src)
	if err != nil {
		return err
	}
	if !recursive && srcNode.IsDir() && len(srcNode.Children) > 0 {
		return ErrNotEmptyDir
	}
	// Copying reads the source's content.
	if err := f.checkPerm(srcNode, permRead); err != nil {
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
		return ErrRootRemove
	}
	// A rename is a write on the directory the entry leaves + one on the directory it lands in
	if err := f.checkPerm(srcNode.Parent, permWrite|permExec); err != nil {
		return err
	}
	parent, name, err := f.destination(src, dst)
	if err != nil {
		return err
	}
	if err := f.checkPerm(parent, permWrite|permExec); err != nil {
		return err
	}
	srcNode.Parent.removeChild(srcNode.Name)
	srcNode.Name = name
	parent.AddChild(srcNode)
	return nil
}

func (f *VFS) Chmod(path string, mode FileMode) error {
	node, err := f.Resolve(path)
	if err != nil {
		return err
	}
	return f.ChmodNode(node, mode)
}

func (f *VFS) ChmodNode(n *Node, mode FileMode) error {
	if err := f.checkOwnership(n); err != nil {
		return err
	}
	// Clear existing permission bits
	n.Mode &= ^FileMode(0777)
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
	if n == nil {
		return ErrNotExist
	}

	if !f.id.IsRoot() {
		if n.Owner.Name != f.id.Name || !f.id.InGroup(n.Owner.Primary()) {
			return ErrNotOwner
		}
	}
	n.Owner = owner
	return nil
}

func (f *VFS) Touch(path string) error {
	node, err := f.Resolve(path)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return f.Create(path)
		}
		return err
	}
	if err := f.checkPerm(node, permWrite); err != nil {
		return err
	}
	node.Touch()
	return nil
}
