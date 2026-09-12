package engine

import (
	"bytes"
	"fmt"
	"io"
	"parrot/internal/engine/commands"
	"parrot/internal/engine/parser"
	"parrot/internal/engine/vfs"
	"slices"
	"strings"
	"sync"
)

const ShellName = "prt"

const exitNotFound = 127

type App struct {
	session *Session
}

func NewApp() *App {
	return &App{NewSession()}
}

func (app *App) Snapshot() ([]byte, error) {
	return app.session.Snapshot()
}

func (app *App) Restore(data []byte) error {
	session, err := LoadSession(data)
	if err != nil {
		return err
	}
	app.session = session
	return nil
}

type Session struct {
	vfs     *vfs.VFS
	env     map[string]string
	history []string

	user  string
	group string
}

func NewSession() *Session {
	return &Session{
		vfs:   vfs.New(),
		env:   map[string]string{},
		user:  vfs.HomeUser,
		group: vfs.HomeGroup,
	}
}

func (s *Session) User() string { return s.user }

func (s *Session) Group() string { return s.group }

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int    // Unix convention: 0 is success, non-zero is failure
	Cwd      string // lets the UI render the prompt without a second call
}

// runPipeline wires a Pipeline's commands together with io.Pipe and a
// goroutine per stage
func (app *App) runPipeline(pipeline *parser.Pipeline) (stdout, stderr string, exitCode int) {
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
				VFS:     app.session.vfs,
				Stdin:   in,
				Stdout:  out,
				Stderr:  &finalErr,
				Env:     app.session.env,
				History: slices.Clone(app.session.history),
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
		filePath := last.Redirect.FilePath
		app.session.vfs.Create(filePath) // create if not exist
		written := append(finalOut.Bytes(), finalErr.Bytes()...)
		finalOut.Reset()
		var err error
		if last.Redirect.Append {
			err = app.session.vfs.Write(filePath, written, true)
		} else {
			err = app.session.vfs.Write(filePath, written, false)
		}
		if err != nil {
			finalErr.WriteString(err.Error())
			finalErr.WriteRune('\n')
			exitCodes = append(exitCodes, 1)
		} else {
			finalErr.Reset()
		}
	}

	return finalOut.String(), finalErr.String(), exitCodes[len(exitCodes)-1]
}

func (app *App) Execute(line string) Result {

	fields, err := parser.Parse(line)
	if err != nil {
		return Result{
			Stderr:   fmt.Sprintf("%s: %v\n", ShellName, err),
			ExitCode: 2,
			Cwd:      app.session.vfs.Cwd(),
		}
	}
	if len(fields) == 0 {
		return Result{Cwd: app.session.vfs.Cwd()}
	}

	app.session.history = append(app.session.history, line)

	goCommands, result := app.GetGoCommands(fields)
	if result != nil {
		return *result
	}

	var stdout, stderr string
	exitCode := 0
	for _, pipeline := range goCommands.Pipelines {
		stdout, stderr, exitCode = app.runPipeline(&pipeline)
		if exitCode != 0 {
			break
		}
	}
	return Result{Stdout: stdout, Stderr: stderr, ExitCode: exitCode, Cwd: app.session.vfs.Cwd()}
}

func (app *App) Upload(path string, data []byte) Result {
	if err := app.session.vfs.Create(path); err != nil {
		return Result{Stderr: fmt.Sprintf("upload: %s: %v\n", path, err), ExitCode: 1, Cwd: app.session.vfs.Cwd()}
	}
	if err := app.session.vfs.Write(path, data, false); err != nil {
		return Result{Stderr: fmt.Sprintf("upload: %s: %v\n", path, err), ExitCode: 1, Cwd: app.session.vfs.Cwd()}
	}
	return Result{ExitCode: 0, Cwd: app.session.vfs.Cwd()}
}

func (app *App) GetGoCommands(fields []*parser.Token) (*parser.AndList, *Result) {
	goCommands := parser.NewAndList(make([]parser.Pipeline, 0))
	begin := 0
	for i, arg := range fields {
		if arg.Kind == parser.And {
			pipeline, res := app.GetPipeline(fields[begin:i])
			if res != nil {
				return nil, res
			}
			goCommands.AddPipeline(*pipeline)
			begin = i + 1
		}
	}
	pipeline, res := app.GetPipeline(fields[begin:])
	if res != nil {
		return nil, res
	}
	goCommands.AddPipeline(*pipeline)
	return goCommands, nil
}

func (app *App) GetPipeline(fields []*parser.Token) (*parser.Pipeline, *Result) {
	pipeline := parser.NewPipeline(make([]parser.SimpleCommand, 0))
	begin := 0
	for i, arg := range fields {
		if arg.Kind == parser.Pipe {
			command, res := app.GetCommands(fields[begin:i])
			if res != nil {
				return nil, res
			}
			pipeline.AddCommand(*command)
			begin = i + 1
		}
	}
	command, res := app.GetCommands(fields[begin:])
	if res != nil {
		return nil, res
	}
	pipeline.AddCommand(*command)
	return pipeline, nil
}

func (app *App) GetCommands(fields []*parser.Token) (*parser.SimpleCommand, *Result) {
	if len(fields) == 0 {
		return nil, &Result{
			Stderr:   fmt.Sprintf("%s: syntax error near unexpected token\n", ShellName),
			ExitCode: 2,
			Cwd:      app.session.vfs.Cwd(),
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
				Cwd:      app.session.vfs.Cwd(),
			}
		}
		file := fields[i+1]
		redirect.FilePath = file.Value
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
			Cwd:      app.session.vfs.Cwd(),
		}
	}
	return parser.NewSimpleCommand(cmd, args, redirect), nil
}

func (app *App) Cwd() string {
	return app.session.Cwd()
}

func (s *Session) Cwd() string {
	return s.vfs.Cwd()
}

func (app *App) Complete(prefix string) []string {
	return app.session.Complete(prefix)
}

func (s *Session) Complete(prefix string) []string {
	nodes, err := s.vfs.List(s.vfs.Cwd())
	if err != nil {
		return nil
	}
	var matches []string
	for _, node := range nodes {
		if strings.HasPrefix(node.Name, ".") {
			continue
		}
		if !strings.HasPrefix(node.Name, prefix) {
			continue
		}
		matches = append(matches, node.Name)
	}
	return matches
}
