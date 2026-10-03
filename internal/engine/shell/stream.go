package shell

import (
	"context"
	"io"

	"parrot/internal/engine/signal"
)

// A guard also calls tick, the host's chance to give the page a turn. It is
// nil off the browser, where there is no event loop to hand control back to.
type interruptReader struct {
	r    io.Reader
	ctx  context.Context
	tick func()
}

func (ir interruptReader) Read(p []byte) (int, error) {
	if ir.tick != nil {
		ir.tick()
	}
	if ir.ctx.Err() != nil {
		return 0, signal.ErrInterrupted
	}
	return ir.r.Read(p)
}

type interruptWriter struct {
	w    io.Writer
	ctx  context.Context
	tick func()
}

func (iw interruptWriter) Write(p []byte) (int, error) {
	if iw.tick != nil {
		iw.tick()
	}
	if iw.ctx.Err() != nil {
		return 0, signal.ErrInterrupted
	}
	return iw.w.Write(p)
}

// A context that can never be cancelled gets no wrapper at all
func GuardReader(ctx context.Context, tick func(), r io.Reader) io.Reader {
	if r == nil || ctx == nil || ctx.Done() == nil {
		return r
	}
	return interruptReader{r: r, ctx: ctx, tick: tick}
}

// it catches a command that produces endlessly without ever reading
func GuardWriter(ctx context.Context, tick func(), w io.Writer) io.Writer {
	if w == nil || ctx == nil || ctx.Done() == nil {
		return w
	}
	return interruptWriter{w: w, ctx: ctx, tick: tick}
}
