package engine

import (
	"bytes"
	"fmt"
	"strings"

	"parrot/internal/engine/shell"
	"parrot/internal/engine/vfs"
)

const ShellName = "prt"

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

// Session is one shell and the filesystem it runs on.
type Session struct {
	fs    *vfs.VFS
	shell *shell.Shell
}

func NewSession() *Session {
	filesystem := vfs.New()
	return &Session{fs: filesystem, shell: shell.New(filesystem)}
}

// Result is what one line of input produced.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int    // Unix convention: 0 is success, non-zero is failure
	Cwd      string // lets the UI render the prompt without a second call
	Exited   bool   // the script asked the shell to close
}

// Execute runs one line of input and collects everything it wrote.
func (app *App) Execute(line string) Result {
	session := app.session
	if strings.TrimSpace(line) == "" {
		return Result{Cwd: session.fs.Cwd()}
	}
	session.shell.History = append(session.shell.History, line)
	return session.run(line)
}

// RunScript runs a whole script, with args as $1 onwards.
func (app *App) RunScript(src string, args []string) Result {
	app.session.shell.SetParams(args)
	return app.session.run(src)
}

func (s *Session) run(src string) Result {
	var stdout, stderr bytes.Buffer
	streams := shell.Streams{In: strings.NewReader(""), Out: &stdout, Err: &stderr}

	status := s.shell.Run(src, streams)
	code, exited := s.shell.Exiting()
	if exited {
		status = code
	}

	return Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: status,
		Cwd:      s.fs.Cwd(),
		Exited:   exited,
	}
}

// Upload writes a file into the session's filesystem.
func (app *App) Upload(path string, data []byte) Result {
	if err := app.session.fs.Create(path); err != nil {
		return Result{Stderr: fmt.Sprintf("upload: %s: %v\n", path, err), ExitCode: 1, Cwd: app.session.fs.Cwd()}
	}
	if err := app.session.fs.Write(path, data, false); err != nil {
		return Result{Stderr: fmt.Sprintf("upload: %s: %v\n", path, err), ExitCode: 1, Cwd: app.session.fs.Cwd()}
	}
	return Result{ExitCode: 0, Cwd: app.session.fs.Cwd()}
}

func (app *App) Cwd() string { return app.session.Cwd() }

func (s *Session) Cwd() string { return s.fs.Cwd() }

// Complete offers the names in the working directory that extend a prefix.
func (app *App) Complete(prefix string) []string {
	return app.session.Complete(prefix)
}

func (s *Session) Complete(prefix string) []string {
	nodes, err := s.fs.List(s.fs.Cwd())
	if err != nil {
		return nil
	}

	var matches []string
	for _, node := range nodes {
		if strings.HasPrefix(node.Name, ".") || !strings.HasPrefix(node.Name, prefix) {
			continue
		}
		matches = append(matches, node.Name)
	}
	return matches
}
