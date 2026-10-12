package vfs

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"parrot/internal/engine/clock"
	"parrot/internal/engine/dev"
	"parrot/internal/engine/filesystem"
	"parrot/internal/engine/user"
)

const (
	currDir = "."
	preDir  = ".."
)

type Ino uint64

// nextIno stands in for the inode counter a superblock keeps.
var nextIno atomic.Uint64

func allocIno() Ino { return Ino(nextIno.Add(1)) }

// reserveIno lifts the counter past a number a restored snapshot already
// uses, so a later allocation cannot collide with it.
func reserveIno(used Ino) {
	for {
		current := nextIno.Load()
		if uint64(used) <= current {
			return
		}
		if nextIno.CompareAndSwap(current, uint64(used)) {
			return
		}
	}
}

type Ownership = filesystem.Ownership

func OwnedBy(id user.Identity) Ownership {
	return Ownership{User: id.Name, Group: id.Primary()}
}

// Inode is a file: its metadata and its contents. It has no name, because a
// name is not a property of a file — it is what a directory calls one of the
// files it points at.
type Inode struct {
	ino Ino

	mu      sync.RWMutex
	mode    filesystem.FileMode // the type bits and the nine permission bits
	owner   Ownership
	mtime   time.Time
	nlink   int
	content []byte            // a regular file's bytes
	entries map[string]*Inode // a directory's entries, including "." and ".."

	// device is what a device file opens onto.
	device dev.Device
}

// Dirent is one directory entry: a name, and the inode it points at.
type Dirent struct {
	Name  string
	Inode *Inode
}

func NewFile(content []byte, owner Ownership) *Inode {
	return &Inode{
		ino:     allocIno(),
		mode:    filesystem.DefaultFileMode,
		owner:   owner,
		mtime:   clock.Now(),
		content: content,
	}
}

func NewDir(owner Ownership) *Inode {
	dir := &Inode{
		ino:     allocIno(),
		mode:    filesystem.DefaultDirMode,
		owner:   owner,
		mtime:   clock.Now(),
		entries: map[string]*Inode{},
		nlink:   1, // "."
	}
	dir.entries[currDir] = dir
	return dir
}

func NewDevice(device dev.Device, owner Ownership, perm filesystem.FileMode) *Inode {
	return &Inode{
		ino:    allocIno(),
		mode:   filesystem.ModeCharDevice | perm&0777,
		owner:  owner,
		mtime:  clock.Now(),
		device: device,
	}
}

func newRoot(owner Ownership) *Inode {
	root := NewDir(owner)
	root.entries[preDir] = root
	root.nlink++
	return root
}

// lockPair takes both locks in inode-number order and gives back the release.
func lockPair(a, b *Inode) func() {
	if a == b {
		a.mu.Lock()
		return a.mu.Unlock
	}
	first, second := a, b
	if first.ino > second.ino {
		first, second = second, first
	}
	first.mu.Lock()
	second.mu.Lock()
	return func() {
		second.mu.Unlock()
		first.mu.Unlock()
	}
}

func (node *Inode) Ino() Ino { return node.ino }

func (node *Inode) lock(do func()) {
	node.mu.Lock()
	defer node.mu.Unlock()
	do()
}

func (node *Inode) Mode() filesystem.FileMode {
	node.mu.RLock()
	defer node.mu.RUnlock()
	return node.mode
}

func (node *Inode) IsDir() bool { return node.Mode().IsDirectory() }

func (node *Inode) Device() dev.Device { return node.device }

func (node *Inode) Owner() Ownership {
	node.mu.RLock()
	defer node.mu.RUnlock()
	return node.owner
}

func (node *Inode) meta() (Ownership, filesystem.FileMode) {
	node.mu.RLock()
	defer node.mu.RUnlock()
	return node.owner, node.mode
}

func (node *Inode) Nlink() int {
	node.mu.RLock()
	defer node.mu.RUnlock()
	return node.nlink
}

func (node *Inode) ModTime() time.Time {
	node.mu.RLock()
	defer node.mu.RUnlock()
	return node.mtime
}

func (node *Inode) Touch() {
	node.lock(func() { node.touch() })
}

// touch is Touch for a caller already holding the lock.
func (node *Inode) touch() { node.mtime = clock.Now() }

func (node *Inode) setOwner(owner Ownership, mode filesystem.FileMode) *Inode {
	node.lock(func() { node.owner, node.mode = owner, mode })
	return node
}

func (node *Inode) setPerm(mode filesystem.FileMode) {
	node.lock(func() {
		node.mode &= ^filesystem.FileMode(0777)
		node.mode |= mode & 0777
	})
}

func (node *Inode) chown(owner, group string) {
	node.lock(func() {
		if owner != "" {
			node.owner.User = owner
		}
		if group != "" {
			node.owner.Group = group
		}
	})
}

func (node *Inode) Bytes() []byte {
	node.mu.RLock()
	defer node.mu.RUnlock()
	return node.content
}

func (node *Inode) Size() int64 {
	node.mu.RLock()
	defer node.mu.RUnlock()
	return int64(len(node.content))
}

func (node *Inode) Override(content []byte) {
	node.lock(func() {
		node.content = content
		node.touch()
	})
}

func (node *Inode) Append(content []byte) {
	node.lock(func() {
		node.content = node.grow(len(node.content), content)
		node.touch()
	})
}

func (node *Inode) ReadAt(p []byte, off int64) int {
	node.mu.RLock()
	defer node.mu.RUnlock()
	if off < 0 || off >= int64(len(node.content)) {
		return 0
	}
	return copy(p, node.content[off:])
}

func (node *Inode) WriteAt(p []byte, off int64) int {
	if off < 0 {
		return 0
	}
	node.lock(func() {
		node.content = node.grow(int(off), p)
		node.touch()
	})
	return len(p)
}

func (node *Inode) grow(at int, p []byte) []byte {
	grown := make([]byte, max(at+len(p), len(node.content)))
	copy(grown, node.content)
	copy(grown[at:], p)
	return grown
}

func (node *Inode) Truncate(size int64) {
	if size < 0 {
		return
	}
	node.lock(func() {
		content := make([]byte, size)
		copy(content, node.content)
		node.content = content
		node.touch()
	})
}

func (node *Inode) Lookup(name string) (*Inode, bool) {
	node.mu.RLock()
	defer node.mu.RUnlock()
	child, ok := node.entries[name]
	return child, ok
}

func (node *Inode) Entries() []Dirent {
	node.mu.RLock()
	entries := make([]Dirent, 0, len(node.entries))
	for name, child := range node.entries {
		if name == currDir || name == preDir {
			continue
		}
		entries = append(entries, Dirent{Name: name, Inode: child})
	}
	node.mu.RUnlock()

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

func (node *Inode) NumEntries() int {
	node.mu.RLock()
	defer node.mu.RUnlock()
	n := 0
	for name := range node.entries {
		if name != currDir && name != preDir {
			n++
		}
	}
	return n
}

func (dir *Inode) Link(name string, child *Inode) error {
	if name == currDir || name == preDir {
		return filesystem.ErrExists
	}
	if dir == child {
		return filesystem.ErrInvalid // a directory cannot be its own entry
	}

	unlock := lockPair(dir, child)
	defer unlock()

	if !dir.mode.IsDirectory() {
		return filesystem.ErrNotDir
	}
	if _, exists := dir.entries[name]; exists {
		return filesystem.ErrExists
	}

	dir.entries[name] = child
	child.nlink++
	if child.mode.IsDirectory() {
		child.entries[preDir] = dir
		dir.nlink++ // the child's ".." is a link back to here
	}
	dir.touch()
	return nil
}

// Unlink drops name and the link it held.
func (dir *Inode) Unlink(name string) error {
	child, ok := dir.Lookup(name)
	if !ok || name == currDir || name == preDir {
		return filesystem.ErrNotExist
	}

	unlock := lockPair(dir, child)
	defer unlock()

	// Re-check: the entry may have gone or been replaced while we had no lock.
	if current, ok := dir.entries[name]; !ok || current != child {
		return filesystem.ErrNotExist
	}

	delete(dir.entries, name)
	child.nlink--
	if child.mode.IsDirectory() {
		delete(child.entries, preDir)
		dir.nlink--
	}
	dir.touch()
	return nil
}

// Path rebuilds a path to this inode by walking ".." and asking each parent
func (node *Inode) Path() string {
	if !node.IsDir() {
		return ""
	}

	var parts []string
	for current := node; ; {
		parent, ok := current.Lookup(preDir)
		if !ok || parent == current {
			break // the root is its own parent
		}
		name := ""
		for _, entry := range parent.Entries() {
			if entry.Inode == current {
				name = entry.Name
				break
			}
		}
		if name == "" {
			break // unlinked while we walked; the path is gone
		}
		parts = append(parts, name)
		current = parent
	}

	if len(parts) == 0 {
		return "/"
	}
	// The walk yields leaf-to-root; a path reads root-to-leaf.
	var b []byte
	for i := len(parts) - 1; i >= 0; i-- {
		b = append(append(b, '/'), parts[i]...)
	}
	return string(b)
}

func (node *Inode) Walk(path string, do func(path string, n *Inode) error) error {
	if err := do(path, node); err != nil {
		return err
	}
	prefix := path
	if prefix == "/" {
		prefix = ""
	}
	for _, entry := range node.Entries() {
		if err := entry.Inode.Walk(prefix+"/"+entry.Name, do); err != nil {
			return err
		}
	}
	return nil
}

func (node *Inode) Clone() *Inode {
	owner, mode := node.meta()
	if node.device != nil {
		return NewDevice(node.device, owner, mode)
	}
	if !mode.IsDirectory() {
		return NewFile(node.Bytes(), owner).setOwner(owner, mode)
	}

	clone := NewDir(owner).setOwner(owner, mode)
	for _, entry := range node.Entries() {
		_ = clone.Link(entry.Name, entry.Inode.Clone())
	}
	return clone
}
