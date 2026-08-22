package engine

import (
	"bytes"
	"fmt"
	"io"
	"parrot/internal/engine/commands"
	"parrot/internal/engine/parser"
	"parrot/internal/engine/vfs"
	"slices"
	"sync"
)

const ShellName = "prt"

const exitNotFound = 127

type Session struct {
	vfs     *vfs.VFS
	env     map[string]string
	history []string
}

func NewSession() *Session {
	return &Session{
		vfs: vfs.New(),
		env: map[string]string{},
	}
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int    // Unix convention: 0 is success, non-zero is failure
	Cwd      string // lets the UI render the prompt without a second call
}

// runPipeline wires a Pipeline's commands together with io.Pipe and a
// goroutine per stage
func (s *Session) runPipeline(pipeline *parser.Pipeline) (stdout, stderr string, exitCode int) {
	var finalOut, finalErr bytes.Buffer
	var wg sync.WaitGroup
	exitCodes := make([]int, len(pipeline.Commands))

	var stdin io.Reader // the first stage gets no piped input
	for i, cmd := range pipeline.Commands {
		isLast := i == len(pipeline.Commands)-1

		var out io.Writer = &finalOut
		var pw *io.PipeWriter
		var nextStdin io.Reader

		if !isLast {
			var pr *io.PipeReader
			pr, pw = io.Pipe()
			out = pw       // this stage's stdout feeds the pipe...
			nextStdin = pr // ...and the next stage reads it as stdin
		}

		wg.Add(1)
		go func(i int, command parser.SimpleCommand, in io.Reader, out io.Writer, pw *io.PipeWriter) {
			defer wg.Done()
			if pw != nil {
				// Closing signals EOF
				defer pw.Close()
			}
			ctx := &commands.Context{
				VFS:     s.vfs,
				Stdin:   in,
				Stdout:  out,
				Stderr:  &finalErr,
				Env:     s.env,
				History: slices.Clone(s.history),
			}
			exitCodes[i] = command.Cmd.Run(ctx, command.Args)
			if in != nil {
				// A command that ignores Stdin must not leave the previous stage's writer
				io.Copy(io.Discard, in)
			}
		}(i, cmd, stdin, out, pw)

		stdin = nextStdin
	}

	wg.Wait()

	if last := pipeline.Commands[len(pipeline.Commands)-1]; last.Redirect != nil {
		written := append(finalOut.Bytes(), finalErr.Bytes()...)
		if last.Redirect.Append {
			last.Redirect.File.Append(written)
		} else {
			last.Redirect.File.Override(written)
		}
		finalOut.Reset()
		finalErr.Reset()
	}

	return finalOut.String(), finalErr.String(), exitCodes[len(exitCodes)-1]
}

func (s *Session) Execute(line string) Result {

	fields, err := parser.Parse(line)
	if err != nil {
		return Result{
			Stderr:   fmt.Sprintf("%s: %v\n", ShellName, err),
			ExitCode: 2,
			Cwd:      s.vfs.Cwd(),
		}
	}
	if len(fields) == 0 {
		return Result{Cwd: s.vfs.Cwd()}
	}

	s.history = append(s.history, line)

	goCommands, result := s.GetGoCommands(fields)
	if result != nil {
		return *result
	}

	var stdout, stderr string
	exitCode := 0
	for _, pipeline := range goCommands.Pipelines {
		stdout, stderr, exitCode = s.runPipeline(&pipeline)
		if exitCode != 0 {
			break
		}
	}
	return Result{Stdout: stdout, Stderr: stderr, ExitCode: exitCode, Cwd: s.vfs.Cwd()}
}

func (s *Session) GetGoCommands(fields []*parser.Token) (*parser.AndList, *Result) {
	goCommands := parser.NewAndList(make([]parser.Pipeline, 0))
	begin := 0
	for i, arg := range fields {
		if arg.Kind == parser.And {
			pipeline, res := s.GetPipeline(fields[begin:i])
			if res != nil {
				return nil, res
			}
			goCommands.AddPipeline(*pipeline)
			begin = i + 1
		}
	}
	pipeline, res := s.GetPipeline(fields[begin:])
	if res != nil {
		return nil, res
	}
	goCommands.AddPipeline(*pipeline)
	return goCommands, nil
}

func (s *Session) GetPipeline(fields []*parser.Token) (*parser.Pipeline, *Result) {
	pipeline := parser.NewPipeline(make([]parser.SimpleCommand, 0))
	begin := 0
	for i, arg := range fields {
		if arg.Kind == parser.Pipe {
			command, res := s.GetCommands(fields[begin:i])
			if res != nil {
				return nil, res
			}
			pipeline.AddCommand(*command)
			begin = i + 1
		}
	}
	command, res := s.GetCommands(fields[begin:])
	if res != nil {
		return nil, res
	}
	pipeline.AddCommand(*command)
	return pipeline, nil
}

func (s *Session) GetCommands(fields []*parser.Token) (*parser.SimpleCommand, *Result) {
	if len(fields) == 0 {
		return nil, &Result{
			Stderr:   fmt.Sprintf("%s: syntax error near unexpected token\n", ShellName),
			ExitCode: 2,
			Cwd:      s.vfs.Cwd(),
		}
	}
	name, fields := fields[0], fields[1:]

	var redirect *parser.RedirectFile
	for i, arg := range fields {
		if arg.Kind != parser.Redirect && arg.Kind != parser.Append {
			continue
		}
		redirect = &parser.RedirectFile{Append: arg.Kind == parser.Append}
		if i != len(fields)-2 {
			redirectToken := ">"
			if redirect.Append {
				redirectToken = ">>"
			}
			return nil, &Result{
				Stderr:   fmt.Sprintf("%s: %s: syntax error near unexpected token `%s'\n", ShellName, name.Value, redirectToken),
				ExitCode: 2,
				Cwd:      s.vfs.Cwd(),
			}
		}
		file := fields[i+1]
		node, err := s.vfs.Resolve(file.Value)
		if err != nil {
			if err = s.vfs.Create(file.Value); err == nil {
				node, err = s.vfs.Resolve(file.Value)
			}
		}
		if err != nil {
			return nil, &Result{
				Stderr:   fmt.Sprintf("%s: %s: %v\n", ShellName, file.Value, err),
				ExitCode: 1,
				Cwd:      s.vfs.Cwd(),
			}
		}
		redirect.File = node
		fields = fields[:i]
		break
	}

	args := make([]string, len(fields))
	for i, arg := range fields {
		args[i] = arg.Value
	}
	cmd, exist := commands.Lookup(name.Value)
	if !exist {
		return nil, &Result{
			Stderr:   fmt.Sprintf("%s: %s: command not found\n", ShellName, name.Value),
			ExitCode: exitNotFound,
			Cwd:      s.vfs.Cwd(),
		}
	}
	return parser.NewSimpleCommand(cmd, args, redirect), nil
}

func (s *Session) Cwd() string {
	return s.vfs.Cwd()
}
