package vfs

import (
	"sort"
	"strings"
)

type VFS struct {
	root *Node
	cwd  *Node
}

// New builds the seed filesystem described in Project.md and starts the
// session in the user's home directory.
//
// Construction is deterministic: no clock, no randomness, nothing read from
// the environment. Two calls produce identical trees, which is what makes the
// tests and the terminal itself reproducible.
//
// Content here is structural placeholder only. The book-club material and the
// jokes arrive in Milestone 8 as embedded data files, never hardcoded.
func New() *VFS {
	root := NewDir("")

	for _, dir := range []string{
		"home", "books", "club", "projects", "memories", "downloads", "secrets",
	} {
		root.AddChild(NewDir(dir))
	}

	home := root.Children["home"]
	home.AddChild(NewDir("friend"))

	root.AddChild(NewFile("README.md", []byte(
		"Welcome.\n\nYou are in a terminal that isn't quite real.\nTry: ls, cd, cat\n",
	)))

	return &VFS{
		root: root,
		cwd:  home.Children["friend"],
	}
}

func (f *VFS) Cwd() string {
	return f.cwd.Path()
}

// Resolve turns a path string into a node.
//
// An empty path means the current directory. A path starting with "/" walks
// from the root, anything else from the cwd. "." is a no-op and ".." moves to
// the parent, staying at the root when already there — as real Unix does.
//
// The walk is deliberate rather than lexical: path.Clean would rewrite
// "/a/../b" to "/b" without ever checking that "a" exists, which is not what
// a filesystem does.
func (f *VFS) Resolve(path string) (*Node, error) {
	curr := f.cwd
	if strings.HasPrefix(path, "/") {
		curr = f.root
	}

	for _, seg := range strings.Split(path, "/") {
		// Split yields empty segments for a leading "/", a trailing "/",
		// and any "//" run. All of them mean "stay here".
		if seg == "" || seg == "." {
			continue
		}

		if seg == ".." {
			if curr.Parent != nil {
				curr = curr.Parent
			}
			continue
		}

		// Only a directory can have anything below it. Checking here rather
		// than after the lookup is what distinguishes "/README.md/x"
		// (not a directory) from "/nope/x" (no such file).
		if !curr.IsDir {
			return nil, ErrNotDir
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
	if !dir.IsDir {
		return ErrNotDir
	}
	// Assigned only after both checks pass, so a failed Chdir leaves the
	// cwd untouched rather than half-moved.
	f.cwd = dir
	return nil
}

// splitParent resolves the parent directory of path and returns it alongside
// the final segment, ready to be linked in.
//
// Taking the node to create as an argument — rather than a string naming its
// kind — means there is no invalid input to handle, and so no error path that
// only a bug could reach.
func (f *VFS) splitParent(path string) (parent *Node, name string, err error) {
	// Trailing slashes name the same directory; "/books/" has no final
	// segment of its own.
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		return nil, "", ErrExists // the root always exists
	}

	// A bare name like "notes.txt" has no parent segment, so its parent is
	// wherever we are standing — "." — not the root.
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
	if !parent.IsDir {
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
	parent.AddChild(NewDir(name))
	return nil
}

func (f *VFS) Create(path string) error {
	parent, name, err := f.splitParent(path)
	if err != nil {
		return err
	}
	parent.AddChild(NewFile(name, nil))
	return nil
}

func (f *VFS) Write(path string, b []byte) error {
	nodeFile, err := f.Resolve(path)
	if err != nil {
		return err
	}
	if nodeFile.IsDir {
		return ErrIsDir
	}
	nodeFile.Content = b
	return nil

}

func (f *VFS) Read(path string) ([]byte, error) {
	nodeFile, err := f.Resolve(path)
	if err != nil {
		return nil, err
	}
	if nodeFile.IsDir {
		return nil, ErrIsDir
	}
	return nodeFile.Content, nil
}

func (f *VFS) List(path string) ([]*Node, error) {
	nodeFile, err := f.Resolve(path)
	if err != nil {
		return nil, err
	}
	if !nodeFile.IsDir {
		return []*Node{nodeFile}, nil
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
	if node.IsDir && len(node.Children) > 0 && !recursive {
		return ErrNotEmptyDir
	}
	node.Parent.removeChild(node.Name)
	return nil
}

func (f *VFS) Copy(src, dst string, recursive bool) error {
	srcNode, err := f.Resolve(src)
	if err != nil {
		return ErrNotExist
	}
	dstNode, err := f.Resolve(dst)
	if err != nil {
		return ErrNotExist
	}
	if !recursive && len(srcNode.Children) > 0 {
		return ErrNotEmptyDir
	}
	newNode := srcNode.Clone()
	dstNode.AddChild(newNode)
	return nil
}

func (f *VFS) Move(src, dst string) error {
	srcNode, err := f.Resolve(src)
	if err != nil {
		return ErrNotExist
	}
	dstNode, err := f.Resolve(dst)
	if err != nil {
		return ErrNotExist
	}
	if srcNode.Path() == "/" {
		return ErrRootRemove
	}
	srcNode.Parent.removeChild(srcNode.Name)
	dstNode.AddChild(srcNode)
	return nil
}
