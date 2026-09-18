package engine

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"parrot/internal/engine/commands"
	"parrot/internal/engine/shell"
	"parrot/internal/engine/user"
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
	users *user.DB
}

func NewSession() *Session {
	filesystem := vfs.New()
	s := &Session{
		fs: filesystem, shell: shell.New(filesystem, nil),
	}
	s.shell.SetUser = s.SetUser
	s.users = user.NewDB(filesystem)
	s.SetUser(vfs.HomeUser)
	return s
}

func (s *Session) SetUser(name string) error {
	id, err := s.users.Lookup(name)
	if err != nil {
		return err
	}
	s.fs.SetIdentity(id)
	return nil
}

func (s *Session) User() string { return s.fs.Identity().Name }

func (s *Session) Group() string { return s.fs.Identity().Primary() }

// Result is what one line of input produced.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int    // Unix convention: 0 is success, non-zero is failure
	Cwd      string // lets the UI render the prompt without a second call
	Exited   bool   // the script asked the shell to close
	User     string // ...and who it belongs to, so `su` is visible in it
}

// Execute runs one line of input and collects everything it wrote.
func (app *App) Execute(line string) Result {
	session := app.session
	if strings.TrimSpace(line) == "" {
		return Result{Cwd: session.fs.Cwd(), User: app.session.User()}
	}
	session.shell.History = append(session.shell.History, commands.HistoryEntry{Line: line, At: time.Now()})
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
		User:     s.User(),
	}
}

// Upload writes a file into the session's filesystem.
func (app *App) Upload(path string, data []byte) Result {
	if err := app.session.fs.Create(path); err != nil {
		return Result{Stderr: fmt.Sprintf("upload: %s: %v\n", path, err), ExitCode: 1, Cwd: app.session.fs.Cwd(), User: app.session.User()}
	}
	if err := app.session.fs.Write(path, data, false); err != nil {
		return Result{Stderr: fmt.Sprintf("upload: %s: %v\n", path, err), ExitCode: 1, Cwd: app.session.fs.Cwd(), User: app.session.User()}
	}
	return Result{ExitCode: 0, Cwd: app.session.fs.Cwd(), User: app.session.User()}
}

func (app *App) Cwd() string { return app.session.Cwd() }

func (s *Session) Cwd() string { return s.fs.Cwd() }

func (app *App) User() string {
	return app.session.User()
}

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
