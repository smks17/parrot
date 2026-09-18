package vfs

import (
	"strings"
	"time"
)

type Node struct {
	Name     string
	Content  []byte           // nil for directories
	Children map[string]*Node // nil for files
	Parent   *Node            // nil at root; makes ".." a pointer hop

	Mode  FileMode // rwx bits for owner/group/other
	Owner string   // username, e.g. "mahdi"
	Group string   // group name, e.g. "users"

	ModTime time.Time
}

func NewFile(name string, content []byte) *Node {
	return &Node{
		Name:     name,
		Content:  content,
		Children: nil,
		Parent:   nil,

		Mode:  DefaultFileMode,
		Owner: HomeUser,
		Group: HomeGroup,

		ModTime: time.Now(),
	}
}

func NewDir(name string) *Node {
	return &Node{
		Name:     name,
		Content:  nil,
		Children: map[string]*Node{},
		Parent:   nil,

		Mode:  DefaultDirMode,
		Owner: HomeUser,
		Group: HomeGroup,

		ModTime: time.Now(),
	}
}

func (node *Node) setOwner(owner, group string, mode FileMode) *Node {
	node.Owner, node.Group, node.Mode = owner, group, mode
	return node
}

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
		Group: n.Group,
	}
	// Reparent the CLONED children — reparenting the original tree's
	// children here would silently steal their ".." hops.
	for _, child := range newListChildren {
		child.Parent = newNode
	}
	return newNode
}

func (node *Node) Touch() {
	node.ModTime = time.Now()
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
	Mode  *FileMode `json:"mode,omitempty"`
	Owner string    `json:"owner,omitempty"`
	Group string    `json:"group,omitempty"`

	// Mtime is Unix seconds.
	Mtime int64 `json:"mtime,omitempty"`
}

func (n *Node) Dump() *DumpNode {
	d := &DumpNode{Name: n.Name, Content: n.Content, Mode: &n.Mode, Owner: n.Owner, Group: n.Group}
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
		n = NewDir(d.Name)
	} else {
		n = NewFile(d.Name, d.Content)
	}
	n.Mode = *d.Mode
	n.Owner = d.Owner
	n.Group = d.Group
	if d.Mtime != 0 {
		n.ModTime = time.Unix(d.Mtime, 0)
	}
	for _, child := range d.Children {
		n.AddChild(child.ToNode())
	}
	return n
}

func (n *Node) Walk(do func(node *Node) error) error {
	err := do(n)
	if err != nil {
		return err
	}
	for _, child := range n.Children {
		err = child.Walk(do)
		if err != nil {
			return err
		}
	}
	return nil
}
