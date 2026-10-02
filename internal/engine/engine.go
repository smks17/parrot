package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"parrot/internal/engine/clock"
	"parrot/internal/engine/commands"
	"parrot/internal/engine/shell"
	"parrot/internal/engine/user"
	"parrot/internal/engine/vfs"
)

const ShellName = "prt"

type App struct {
	session *Session
	tick    func()
}

func NewApp() *App { return &App{session: NewSession()} }

// SetYield installs the host's hook for giving its event loop a turn.
func (app *App) SetYield(tick func()) {
	app.tick = tick
	app.session.shell.SetYield(tick)
}

func (app *App) Snapshot() ([]byte, error) {
	return app.session.Snapshot()
}

func (app *App) Restore(data []byte) error {
	session, err := LoadSession(data)
	if err != nil {
		return err
	}
	session.shell.SetYield(app.tick)
	app.session = session
	return nil
}

// Session is one shell and the filesystem it runs on.
type Session struct {
	fs    *vfs.VFS
	shell *shell.Shell
}

func NewSession() *Session { return newSession(vfs.New()) }

func newSession(filesystem *vfs.VFS) *Session {
	s := &Session{fs: filesystem, shell: shell.New(filesystem, nil)}
	s.shell.SwitchUser = s.SwitchUser
	resident, err := filesystem.UsersDB().Resident()
	if err != nil || s.SetUser(resident.Name) != nil {
		s.SetUser(user.RootName)
	}
	return s
}

func (s *Session) SetUser(name string) error {
	id, err := s.fs.UsersDB().Lookup(name)
	if err != nil {
		return err
	}
	s.fs.SetIdentity(id)
	vars := s.shell.Vars()
	vars[commands.EnvUser] = id.Name
	vars[commands.EnvHome] = id.Home
	return nil
}

func (app *App) Login(name, password string) error { return app.session.Login(name, password) }

func (s *Session) Login(name, password string) error {
	if err := s.fs.UsersDB().Authenticate(name, password); err != nil {
		return err
	}
	if err := s.SetUser(name); err != nil {
		return err
	}
	if err := s.fs.Chdir(s.fs.Identity().Home); err != nil {
		s.fs.Chdir("/")
	}
	return nil
}

func (s *Session) SwitchUser(name, password string) error {
	if !s.fs.Identity().IsRoot() {
		if err := s.fs.UsersDB().Authenticate(name, password); err != nil {
			return err
		}
	}
	return s.SetUser(name)
}

func (s *Session) User() string { return s.fs.Identity().Name }

// at is the wall clock the shell believes in — TZ and `date -s` included.
func (s *Session) at() string { return clock.In(s.shell.Vars()["TZ"]).Format("15:04:05") }

// Now is that clock, for callers outside a command's result.
func (app *App) Now() string { return app.session.at() }

// Result is what one line of input produced.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int    // Unix convention: 0 is success, non-zero is failure
	Cwd      string // lets the UI render the prompt without a second call
	Exited   bool   // the script asked the shell to close
	User     string // ...and who it belongs to, so `su` is visible in it
	At       string // the shell clock when it ran, for the UI to stamp the line

	Interrupted bool
}

// Execute runs one line of input and collects everything it wrote.
func (app *App) Execute(line string) Result {
	return app.ExecuteStream(context.Background(), line, nil, nil)
}

func (app *App) ExecuteStream(ctx context.Context, line string, out, errOut io.Writer) Result {
	session := app.session
	if strings.TrimSpace(line) == "" {
		return session.result(0, Result{})
	}
	session.shell.History = append(session.shell.History, commands.HistoryEntry{Line: line, At: clock.Now()})
	return session.runStream(ctx, line, out, errOut)
}

// RunScript runs a whole script, with args as $1 onwards.
func (app *App) RunScript(src string, args []string) Result {
	return app.RunScriptStream(context.Background(), src, args, nil, nil)
}

// RunScriptStream is RunScript with the streaming and cancellation that
// ExecuteStream has.
func (app *App) RunScriptStream(ctx context.Context, src string, args []string, out, errOut io.Writer) Result {
	app.session.shell.SetParams(args)
	return app.session.runStream(ctx, src, out, errOut)
}

// result stamps the session's own state onto a Result the caller has
// already filled in the interesting parts of.
func (s *Session) result(status int, r Result) Result {
	r.ExitCode = status
	r.Cwd = s.fs.Cwd()
	r.User = s.User()
	r.At = s.at()
	return r
}

func (s *Session) runStream(ctx context.Context, src string, out, errOut io.Writer) Result {
	var stdout, stderr bytes.Buffer

	outW, errW := io.Writer(&stdout), io.Writer(&stderr)
	buffered := out == nil && errOut == nil
	if !buffered {
		outW, errW = io.Discard, io.Discard
		if out != nil {
			outW = out
		}
		if errOut != nil {
			errW = errOut
		}
	}

	s.shell.SetContext(ctx)
	defer s.shell.SetContext(context.Background())

	status := s.shell.Run(src, vfs.NewStdTable(strings.NewReader(""), outW, errW))
	code, exited := s.shell.Exiting()
	if exited {
		status = code
	}
	interrupted := s.shell.Interrupted() || ctx.Err() != nil
	if interrupted {
		status = 130
	}

	result := Result{Exited: exited, Interrupted: interrupted}
	if buffered {
		result.Stdout = stdout.String()
		result.Stderr = stderr.String()
	}
	return s.result(status, result)
}

// Upload writes a file into the session's filesystem.
func (app *App) Upload(path string, data []byte) Result {
	if err := app.session.fs.Create(path); err != nil {
		return Result{Stderr: fmt.Sprintf("upload: %s: %v\n", path, err), ExitCode: 1, Cwd: app.session.fs.Cwd(), User: app.session.User(), At: app.session.at()}
	}
	if err := app.session.fs.Write(path, data, false); err != nil {
		return Result{Stderr: fmt.Sprintf("upload: %s: %v\n", path, err), ExitCode: 1, Cwd: app.session.fs.Cwd(), User: app.session.User(), At: app.session.at()}
	}
	return Result{ExitCode: 0, Cwd: app.session.fs.Cwd(), User: app.session.User(), At: app.session.at()}
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
	entries, err := s.fs.List(s.fs.Cwd())
	if err != nil {
		return nil
	}

	var matches []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name, ".") || !strings.HasPrefix(entry.Name, prefix) {
			continue
		}
		matches = append(matches, entry.Name)
	}
	return matches
}
