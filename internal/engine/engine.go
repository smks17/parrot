package engine

import (
	"bytes"
	"fmt"
	"strings"

	"parrot/internal/engine/clock"
	"parrot/internal/engine/commands"
	"parrot/internal/engine/filesystem"
	"parrot/internal/engine/shell"
	"parrot/internal/engine/user"
	"parrot/internal/engine/vfs"
)

const ShellName = "prt"

type App struct {
	session *Session
}

func NewApp() *App {
	users := user.InitUsers([]string{})
	return &App{NewSession(users)}
}

// NewAppOn starts a shell on a filesystem of your choosing
func NewAppOn(fsys filesystem.FS) *App {
	session := &Session{fs: fsys, shell: shell.New(fsys, nil)}
	session.shell.SwitchUser = session.SwitchUser
	id := fsys.Identity()
	vars := session.shell.Vars()
	vars[commands.EnvUser] = id.Name
	vars[commands.EnvHome] = id.Home
	return &App{session: session}
}

func (app *App) Snapshot() ([]byte, error) {
	return app.session.Snapshot()
}

func (app *App) Restore(data []byte, asUser string, rootUser *user.Identity) error {
	session, err := LoadSession(data, asUser, rootUser)
	if err != nil {
		return err
	}
	app.session = session
	return nil
}

// Session is one shell and the filesystem it runs on.
type Session struct {
	fs    filesystem.FS
	shell *shell.Shell
}

func NewSession(users []*user.Identity) *Session {
	fsys := vfs.New(users, users[0], users[0])
	s := &Session{
		fs: fsys, shell: shell.New(fsys, nil),
	}
	s.shell.SwitchUser = s.SwitchUser
	// s.SetUser(vfs.HomeUser)
	return s
}

func (s *Session) tree() (*vfs.VFS, bool) {
	tree, inMemory := s.fs.(*vfs.VFS)
	return tree, inMemory
}

func (s *Session) SetUser(name string) error {
	tree, inMemory := s.tree()
	if !inMemory {
		return ErrRealFilesystem
	}
	id, err := tree.UsersDB().Lookup(name)
	if err != nil {
		return err
	}
	tree.SetIdentity(id)
	vars := s.shell.Vars()
	vars[commands.EnvUser] = id.Name
	vars[commands.EnvHome] = id.Home
	return nil
}

func (app *App) Login(name, password string) error { return app.session.Login(name, password) }

func (s *Session) Login(name, password string) error {
	tree, inMemory := s.tree()
	if !inMemory {
		return ErrRealFilesystem
	}
	if err := tree.UsersDB().Authenticate(name, password); err != nil {
		return err
	}
	if err := s.SetUser(name); err != nil {
		return err
	}
	s.fs.Chdir(s.fs.Identity().Home)
	return nil
}

func (s *Session) SwitchUser(name, password string) error {
	tree, inMemory := s.tree()
	if !inMemory {
		return ErrRealFilesystem
	}
	if !s.fs.Identity().IsRoot() {
		if err := tree.UsersDB().Authenticate(name, password); err != nil {
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

func (s *Session) Group() string { return s.fs.Identity().Primary() }

func (app *App) Root() (user.Identity, error) {
	tree, inMemory := app.session.tree()
	if !inMemory {
		return user.Identity{}, ErrRealFilesystem
	}
	return tree.RootUser()
}

// Result is what one line of input produced.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int    // Unix convention: 0 is success, non-zero is failure
	Cwd      string // lets the UI render the prompt without a second call
	Exited   bool   // the script asked the shell to close
	User     string // ...and who it belongs to, so `su` is visible in it
	At       string // the shell clock when it ran, for the UI to stamp the line
}

// Execute runs one line of input and collects everything it wrote.
func (app *App) Execute(line string) Result {
	session := app.session
	if strings.TrimSpace(line) == "" {
		return Result{Cwd: session.fs.Cwd(), User: app.session.User(), At: session.at()}
	}
	session.shell.History = append(session.shell.History, commands.HistoryEntry{Line: line, At: clock.Now()})
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
		At:       s.at(),
	}
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
