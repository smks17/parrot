package stream

import (
	"io"
	"sync"
)

const PipeSize = 64 << 10

// Buffer holds bytes between whoever writes them and whoever takes them. A
// pipe is one with a reader at the other end. A session's terminal is one
// with an output: while it has one, writes go straight there, and while it
// has none, they wait here until it gets one.
type Buffer struct {
	mu      sync.Mutex
	changed *sync.Cond

	data   []byte
	closed bool
	output io.Writer // when set, writes go straight here instead of into data
}

func NewBuffer() *Buffer {
	buff := &Buffer{}
	buff.changed = sync.NewCond(&buff.mu)
	return buff
}

// Write waits while the pipe is full. Once either end is closed it fails with
// io.ErrClosedPipe, which a File reports as a broken pipe.
func (buff *Buffer) Write(b []byte) (int, error) {
	buff.mu.Lock()
	defer buff.mu.Unlock()
	n := 0
	for len(b) > 0 {
		for buff.output == nil && len(buff.data) >= PipeSize && !buff.closed {
			buff.changed.Wait()
		}
		if buff.closed {
			return n, io.ErrClosedPipe
		}
		if buff.output != nil {
			// Under the lock, so SetOutput can never let a write slip past
			// it to the output it is replacing.
			written, err := buff.output.Write(b)
			return n + written, err
		}
		room := min(len(b), PipeSize-len(buff.data))
		buff.data = append(buff.data, b[:room]...)
		b, n = b[room:], n+room
		buff.changed.Broadcast()
	}
	return n, nil
}

// Read waits while the pipe is empty. Once the writer is gone it returns what
// is left, then io.EOF.
func (buf *Buffer) Read(b []byte) (int, error) {
	buf.mu.Lock()
	defer buf.mu.Unlock()
	for len(buf.data) == 0 && !buf.closed {
		buf.changed.Wait()
	}
	if len(buf.data) == 0 {
		return 0, io.EOF
	}
	n := copy(b, buf.data)
	buf.data = append(buf.data[:0], buf.data[n:]...) // moved down, so the backing array stays PipeSize
	buf.changed.Broadcast()
	return n, nil
}

// SetOutput sends what the buffer holds to w, then every write after it. Nil
// makes writes wait in the buffer again, up to PipeSize. Once it returns,
// nothing more reaches the output it replaced.
func (buf *Buffer) SetOutput(w io.Writer) {
	buf.mu.Lock()
	defer buf.mu.Unlock()
	if w != nil && len(buf.data) > 0 {
		w.Write(buf.data)
		buf.data = buf.data[:0]
	}
	buf.output = w
	buf.changed.Broadcast() // a writer waiting for room can go straight to w now
}

// WriteEnd is a description that writes into the buffer, to seat at a
// process's fd 1 or 2. Closing it leaves the buffer open: other processes
// still write to it.
func (buf *Buffer) WriteEnd() *File {
	return &File{writer: buf, flags: O_WRONLY}
}

func (buf *Buffer) Close() error {
	buf.mu.Lock()
	defer buf.mu.Unlock()
	if buf.closed {
		return ErrBadFD
	}
	buf.closed = true
	buf.changed.Broadcast()
	return nil
}
