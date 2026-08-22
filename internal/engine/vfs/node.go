package vfs

import "strings"

type Node struct {
	Name     string
	IsDir    bool
	Content  []byte           // nil for directories
	Children map[string]*Node // nil for files
	Parent   *Node            // nil at root; makes ".." a pointer hop
}

func NewFile(name string, content []byte) *Node {
	return &Node{
		Name:     name,
		IsDir:    false,
		Content:  content,
		Children: nil,
		Parent:   nil,
	}
}

func NewDir(name string) *Node {
	return &Node{
		Name:     name,
		IsDir:    true,
		Children: map[string]*Node{},
		Parent:   nil,
	}
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
}

func (node *Node) removeChild(name string) {
	delete(node.Children, name)
}

func (n *Node) Clone() *Node {
	newListChildren := make(map[string]*Node)
	for _, child := range n.Children {
		newListChildren[child.Name] = child.Clone()
	}
	newNode := &Node{
		Name:     n.Name,
		IsDir:    n.IsDir,
		Content:  n.Content,
		Children: newListChildren,
		Parent:   nil,
	}
	for _, child := range n.Children {
		child.Parent = newNode
	}
	return newNode
}

func (node *Node) Override(content []byte) {
	node.Content = content
}

func (node *Node) Append(content []byte) {
	node.Content = append(node.Content, content...)
}
