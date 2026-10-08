package proc

import (
	"context"
	"errors"
	"io"

	"parrot/internal/engine/filesystem"
	"parrot/internal/engine/signal"
)

// StreamGuard is what every read and write of a command passes through. It is the
// one place a running command meets the rest of the system: the host gets
// its turn, Ctrl-C and signals take effect, and a wait on a pipe gives the
// CPU to someone else.
type StreamGuard struct {
	Context     context.Context
	YieldToHost func() // the host's chance to give the page a turn; nil off the browser
	Process     *Process
}

func (g StreamGuard) WrapReader(r io.Reader) io.Reader {
	if r == nil || !g.needsGuarding() {
		return r
	}
	return &guardedReader{guard: g, reader: r}
}

func (g StreamGuard) WrapWriter(w io.Writer) io.Writer {
	if w == nil || !g.needsGuarding() {
		return w
	}
	return &guardedWriter{guard: g, writer: w}
}

// needsGuarding reports whether there is anything to guard. A context that can never
// be cancelled and no process means no wrapper at all, which keeps native and
// buffered paths at full speed.
func (g StreamGuard) needsGuarding() bool {
	return g.Process != nil || (g.Context != nil && g.Context.Done() != nil)
}

// yieldAndCheckInterrupt is what happens ahead of every read and write.
func (g StreamGuard) yieldAndCheckInterrupt() error {
	if g.YieldToHost != nil {
		g.YieldToHost()
	}
	if g.Context != nil && g.Context.Err() != nil {
		return signal.ErrInterrupted
	}
	if g.Process != nil && g.Process.checkStopAndPreemption() != nil {
		return signal.ErrInterrupted
	}
	return nil
}

// transferOffCPUIfBlocking runs one read or write. One that may wait on a pipe runs off the CPU, so
// the process does not hold it while it waits.
func (g StreamGuard) transferOffCPUIfBlocking(stream any, transfer func() (int, error)) (n int, err error) {
	if g.Process == nil || !canBlock(stream) {
		return transfer()
	}
	if g.Process.sleepWhile(InterruptSleep, func() { n, err = transfer() }) != nil && err == nil {
		err = signal.ErrInterrupted
	}
	return n, err
}

// canBlock reports whether a read or write on stream can wait. A descriptor
// may be a pipe; a buffer or the terminal never waits.
func canBlock(stream any) bool {
	switch stream.(type) {
	case *filesystem.File, *io.PipeReader, *io.PipeWriter:
		return true
	}
	return false
}

type guardedReader struct {
	guard  StreamGuard
	reader io.Reader
}

func (r *guardedReader) Read(b []byte) (int, error) {
	if err := r.guard.yieldAndCheckInterrupt(); err != nil {
		return 0, err
	}
	return r.guard.transferOffCPUIfBlocking(r.reader, func() (int, error) { return r.reader.Read(b) })
}

type guardedWriter struct {
	guard  StreamGuard
	writer io.Writer
}

func (w *guardedWriter) Write(b []byte) (int, error) {
	if err := w.guard.yieldAndCheckInterrupt(); err != nil {
		return 0, err
	}
	n, err := w.guard.transferOffCPUIfBlocking(w.writer, func() (int, error) { return w.writer.Write(b) })
	if p := w.guard.Process; p != nil && isBrokenPipeError(err) {
		p.table.Kill(nil, p.PID, SIGPIPE)
	}
	return n, err
}

func isBrokenPipeError(err error) bool {
	return errors.Is(err, filesystem.ErrBrokenPipe) || errors.Is(err, io.ErrClosedPipe)
}
