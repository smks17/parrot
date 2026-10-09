package stream

// pipe is a buffer shared by two descriptions, one that reads it and one that
// writes it: what joins the stages of a pipeline.
type pipe struct {
	*Buffer
}

func Pipe() (r *File, w *File) {
	p := &pipe{Buffer: NewBuffer()}
	return &File{reader: p, closer: p, flags: O_RDONLY},
		&File{writer: p, closer: p, flags: O_WRONLY}
}

func (p *pipe) Close() error {
	p.Buffer.Close()
	return nil
}
