package filesystem

import (
	"errors"
	"io"
	"sync"
)

type OpenFlags int

// TODO: Complete via https://man7.org/linux/man-pages/man2/open.2.html#:~:text=DESCRIPTION,-top
const (
	O_RDONLY OpenFlags = 0x0
	O_WRONLY OpenFlags = 0x1
	O_RDWR   OpenFlags = 0x2

	O_CREATE OpenFlags = 0x40
	O_TRUNC  OpenFlags = 0x200
	O_APPEND OpenFlags = 0x400

	accessMask OpenFlags = 0x3
)

func (flags OpenFlags) Readable() bool {
	return flags&accessMask == O_RDONLY || flags&accessMask == O_RDWR
}

func (flags OpenFlags) Writable() bool {
	return flags&accessMask == O_WRONLY || flags&accessMask == O_RDWR
}

// Store is the file a description reads and writes through: an inode in the
// in-memory tree. A stream has none.
type Store interface {
	ReadAt(p []byte, off int64) int
	WriteAt(p []byte, off int64) int
	Append(content []byte)
	Truncate(size int64)
	Size() int64
	IsDir() bool
}

// An open file description
type File struct {
	mu     sync.Mutex
	inode  Store
	offset int64
	flags  OpenFlags
	closed bool

	reader io.Reader
	writer io.Writer
	closer io.Closer
}

func NewStreamFile(r io.Reader, w io.Writer) *File {
	return &File{reader: r, writer: w, flags: O_RDWR}
}

// NewHostFile is a description over a descriptor the machine opened, which is
// how a filesystem made of real files answers Open.
func NewHostFile(f io.ReadWriteCloser, flags OpenFlags) *File {
	return &File{reader: f, writer: f, closer: f, flags: flags}
}

// a one-way channel joining two file descriptions.
func Pipe() (r *File, w *File) {
	pr, pw := io.Pipe()
	return &File{reader: pr, closer: pr, flags: O_RDONLY},
		&File{writer: pw, closer: pw, flags: O_WRONLY}
}

func pipeErr(err error) error {
	if errors.Is(err, io.ErrClosedPipe) {
		return ErrBrokenPipe
	}
	return err
}

func (f *File) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed || !f.flags.Readable() {
		return 0, ErrBadFD
	}
	if f.reader != nil {
		n, err := f.reader.Read(p)
		return n, pipeErr(err)
	}
	if f.inode == nil {
		return 0, ErrBadFD // the write end of a pipe, read from
	}
	n := f.inode.ReadAt(p, f.offset)
	if n == 0 {
		return 0, io.EOF
	}
	f.offset += int64(n)
	return n, nil
}

func (f *File) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed || !f.flags.Writable() {
		return 0, ErrBadFD
	}
	if f.writer != nil {
		n, err := f.writer.Write(p)
		return n, pipeErr(err)
	}
	if f.inode == nil {
		return 0, ErrBadFD // the read end of a pipe, written to
	}

	if f.flags&O_APPEND != 0 {
		f.inode.Append(p)
		f.offset = f.inode.Size()
		return len(p), nil
	}
	n := f.inode.WriteAt(p, f.offset)
	f.offset += int64(n)
	return n, nil
}

// Seek moves a description's offset.
func (f *File) Seek(offset int64, whence int) (int64, error) {
	return 0, ErrNotImplemented
}

// Close drops this description.
func (f *File) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return ErrBadFD
	}
	f.closed = true
	if f.closer != nil {
		return f.closer.Close()
	}
	return nil
}

func OpenFile(node Store, flags OpenFlags) (*File, error) {
	if node.IsDir() {
		return nil, ErrIsDir // a directory is read with List, not with read(2)
	}

	if flags&O_TRUNC != 0 && flags.Writable() {
		node.Truncate(0)
	}
	file := &File{inode: node, flags: flags}
	if flags&O_APPEND != 0 {
		file.offset = node.Size()
	}
	return file, nil
}

// The three descriptors every process is handed.
const (
	Stdin  = 0
	Stdout = 1
	Stderr = 2
)

// FDTable is a process's file descriptor table
type FDTable struct {
	mu    sync.Mutex
	files []*File
}

func NewFDTable(in, out, err *File) *FDTable {
	return &FDTable{files: []*File{in, out, err}}
}

// NewStdTable seats a host reader and two host writers at fds 0, 1 and 2,
func NewStdTable(in io.Reader, out, err io.Writer) *FDTable {
	return NewFDTable(
		NewStreamFile(in, nil),
		NewStreamFile(nil, out),
		NewStreamFile(nil, err),
	)
}

func (t *FDTable) get(fd int) (*File, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if fd < 0 || fd >= len(t.files) || t.files[fd] == nil {
		return nil, ErrBadFD
	}
	return t.files[fd], nil
}

func (t *FDTable) Alloc(file *File) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	for fd, f := range t.files {
		if f == nil {
			t.files[fd] = file
			return fd
		}
	}
	t.files = append(t.files, file)
	return len(t.files) - 1
}

func (t *FDTable) Set(fd int, file *File) {
	if fd < 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for len(t.files) <= fd {
		t.files = append(t.files, nil)
	}
	t.files[fd] = file
}

func (t *FDTable) Dup(oldfd, newfd int) error {
	file, err := t.get(oldfd)
	if err != nil {
		return err
	}
	t.Set(newfd, file)
	return nil
}

func (t *FDTable) Close(fd int) error {
	file, err := t.get(fd)
	if err != nil {
		return err
	}
	err = file.Close()
	if err != nil {
		return err
	}
	t.Set(fd, nil)
	return nil
}

func (t *FDTable) Destroy() error {
	t.mu.Lock()
	files := t.files
	t.files = nil
	t.mu.Unlock()

	var first error
	for _, file := range files {
		if file == nil {
			continue
		}
		if err := file.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (t *FDTable) Clone() *FDTable {
	t.mu.Lock()
	defer t.mu.Unlock()
	return &FDTable{files: append([]*File(nil), t.files...)}
}

func (t *FDTable) Stdin() io.Reader  { return t.Reader(Stdin) }
func (t *FDTable) Stdout() io.Writer { return t.Writer(Stdout) }
func (t *FDTable) Stderr() io.Writer { return t.Writer(Stderr) }

func (t *FDTable) Reader(fd int) io.Reader {
	file, err := t.get(fd)
	if err != nil {
		return badFD{}
	}
	return file
}

func (t *FDTable) Writer(fd int) io.Writer {
	file, err := t.get(fd)
	if err != nil {
		return badFD{}
	}
	return file
}

type badFD struct{}

func (badFD) Read([]byte) (int, error)  { return 0, ErrBadFD }
func (badFD) Write([]byte) (int, error) { return 0, ErrBadFD }
