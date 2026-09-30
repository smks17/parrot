package vfs

import (
	"slices"

	"parrot/internal/engine/clock"
	"parrot/internal/engine/filesystem"
	"parrot/internal/engine/user"
	"strings"
	"time"
)

type Node struct {
	Name     string
	Content  []byte           // nil for directories
	Children map[string]*Node // nil for files
	Parent   *Node            // nil at root; makes ".." a pointer hop

	Mode  filesystem.FileMode // rwx bits for owner/group/other
	Owner user.Identity

	ModTime time.Time
}

func NewFile(name string, content []byte, owner user.Identity) *Node {
	return &Node{
		Name:     name,
		Content:  content,
		Children: nil,
		Parent:   nil,

		Mode:  filesystem.DefaultFileMode,
		Owner: owner,

		ModTime: clock.Now(),
	}
}

func NewDir(name string, owner user.Identity) *Node {
	return &Node{
		Name:     name,
		Content:  nil,
		Children: map[string]*Node{},
		Parent:   nil,

		Mode:  filesystem.DefaultDirMode,
		Owner: owner,

		ModTime: clock.Now(),
	}
}

// TODO
// func (node *Node) setOwner(owner, group string, mode filesystem.FileMode) *Node {
// 	node.Owner, node.Group, node.Mode = owner, group, mode
// 	return node
// }

func (node *Node) IsDir() bool {
	return node.Mode.IsDirectory()
}

// Path walks up through Parent to build the absolute path. The root reports "/".
func (node *Node) Path() string {
	if node.Parent == nil {
		return "/"
	}

	// Collect names on the way up, then reverse: the walk yields
	// leaf-to-root, the path reads root-to-leaf.
	var parts []string
	for n := node; n.Parent != nil; n = n.Parent {
		parts = append(parts, n.Name)
	}
	var b strings.Builder
	for i := len(parts) - 1; i >= 0; i-- {
		b.WriteByte('/')
		b.WriteString(parts[i])
	}
	return b.String()
}

func (node *Node) AddChild(child *Node) {
	node.Children[child.Name] = child
	child.Parent = node
	child.Touch()
	node.Touch()
}

func (node *Node) removeChild(name string) {
	delete(node.Children, name)
	node.Touch()
}

func (n *Node) Clone() *Node {
	newListChildren := make(map[string]*Node)
	for _, child := range n.Children {
		newListChildren[child.Name] = child.Clone()
	}
	newNode := &Node{
		Name:     n.Name,
		Content:  n.Content,
		Children: newListChildren,
		Parent:   nil,

		Mode:  n.Mode,
		Owner: n.Owner,
	}
	// Reparent the CLONED children — reparenting the original tree's
	// children here would silently steal their ".." hops.
	for _, child := range newListChildren {
		child.Parent = newNode
	}
	return newNode
}

func (node *Node) Touch() {
	node.ModTime = clock.Now()
}

func (node *Node) Override(content []byte) {
	node.Content = content
	node.Touch()
}

func (node *Node) Append(content []byte) {
	node.Content = append(node.Content, content...)
	node.Touch()
}

type DumpNode struct {
	Name     string      `json:"name"`
	Content  []byte      `json:"content,omitempty"`
	Children []*DumpNode `json:"children,omitempty"`

	// Mode is a pointer so a fully-chmod'ed-away 000 survives the round
	// trip, while snapshots saved before permissions existed (no "mode"
	// key) restore to the constructor defaults instead of losing theirs.
	Mode  *filesystem.FileMode `json:"mode,omitempty"`
	Owner user.Identity        `json:"owner,omitempty"`

	// Mtime is Unix seconds.
	Mtime int64 `json:"mtime,omitempty"`
}

func (n *Node) Dump() *DumpNode {
	d := &DumpNode{Name: n.Name, Content: n.Content, Mode: &n.Mode, Owner: n.Owner}
	// A zero Time means "unknown"
	if !n.ModTime.IsZero() {
		d.Mtime = n.ModTime.Unix()
	}
	for _, child := range n.Children {
		d.Children = append(d.Children, child.Dump())
	}
	return d
}

func (d *DumpNode) ToNode() *Node {
	var n *Node
	if d.Mode.IsDirectory() {
		n = NewDir(d.Name, d.Owner)
	} else {
		n = NewFile(d.Name, d.Content, d.Owner)
	}
	n.Mode = *d.Mode
	n.Owner = d.Owner
	if d.Mtime != 0 {
		n.ModTime = time.Unix(d.Mtime, 0)
	}
	for _, child := range d.Children {
		n.AddChild(child.ToNode())
	}
	return n
}

func (n *Node) Walk(do func(node *Node) error) error {
	if err := do(n); err != nil {
		return err
	}
	names := make([]string, 0, len(n.Children))
	for name := range n.Children {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		if err := n.Children[name].Walk(do); err != nil {
			return err
		}
	}
	return nil
}

// info projects a node into the shape every filesystem reports.
func info(node *Node) filesystem.Info {
	entry := filesystem.Info{
		Name:    node.Name,
		Path:    node.Path(),
		Mode:    node.Mode,
		Owner:   node.Owner,
		Size:    int64(len(node.Content)),
		Links:   1,
		ModTime: node.ModTime,
	}
	if node.IsDir() {
		// A directory is linked to by itself, by its parent, and by each
		// subdirectory's "..".
		entry.Links = 2
		entry.Size = 0
		for _, child := range node.Children {
			if child.IsDir() {
				entry.Links++
			}
		}
	}
	return entry
}
